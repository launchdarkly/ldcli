package api

import (
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type recordedRequest struct {
	AccessToken string
	Method      string
	Path        string
	ContentType string
	Query       url.Values
	Body        []byte
	IsBeta      bool
}

type recordingClient struct {
	Requests  []recordedRequest
	Responses [][]byte
	Err       error
}

var _ resources.Client = &recordingClient{}

func (client *recordingClient) MakeRequest(
	accessToken string,
	method string,
	path string,
	contentType string,
	query url.Values,
	body []byte,
	isBeta bool,
) ([]byte, error) {
	client.Requests = append(client.Requests, recordedRequest{
		AccessToken: accessToken,
		Method:      method,
		Path:        path,
		ContentType: contentType,
		Query:       query,
		Body:        append([]byte(nil), body...),
		IsBeta:      isBeta,
	})
	if client.Err != nil {
		return nil, client.Err
	}

	return client.Responses[len(client.Requests)-1], nil
}

func (*recordingClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestClientPlan(t *testing.T) {
	transport := &recordingClient{
		Responses: [][]byte{
			[]byte(`{
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "config/first",
					"status": "local_changed",
					"syncDirection": "code_canonical",
					"diff": {"name": {"before": "Old", "after": "First"}}
				}]
			}`),
			[]byte(`{
				"resources": [{
					"resourceKind": "variation",
					"lookupKey": "config/second",
					"status": "server_changed",
					"syncDirection": "server_canonical"
				}]
			}`),
		},
	}
	client := NewClient(transport)
	source := requireSource(t, syncdomain.SourceTypeGit, "github.com/launchdarkly/example")

	plans, err := client.Plan(
		"token",
		"https://example.com",
		source,
		true,
		[]syncdomain.SyncedResource{
			variationResource("alpha", "config/first", "First", true),
			variationResource("zeta", "config/second", "Second", false),
		},
	)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 2)
	require.Len(t, plans, 2)

	firstRequest := transport.Requests[0]
	assert.Equal(t, "POST", firstRequest.Method)
	assert.Equal(t, "token", firstRequest.AccessToken)
	assert.Equal(t, "application/json", firstRequest.ContentType)
	assert.False(t, firstRequest.IsBeta)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/alpha/ai-configs/sync/plan",
		firstRequest.Path,
	)

	var request planRequest
	require.NoError(t, json.Unmarshal(firstRequest.Body, &request))
	assert.Equal(t, syncdomain.SourceTypeGit, request.Source.Type)
	assert.Equal(t, "github.com/launchdarkly/example", request.Source.Identifier)
	assert.True(t, request.DryRun)
	require.Len(t, request.Resources, 1)
	assert.Equal(t, syncdomain.KindVariation, request.Resources[0].ResourceKind)
	assert.Equal(t, "config/first", request.Resources[0].LookupKey)
	assert.True(t, request.Resources[0].Upsert)
	assert.JSONEq(t, `{"key":"first","name":"First"}`, string(request.Resources[0].Payload))
	assert.NotContains(t, string(firstRequest.Body), "fingerprint")
	assert.NotContains(t, string(firstRequest.Body), "repoIdentifier")

	assert.Equal(t, "alpha", plans[0].ProjectKey)
	require.Len(t, plans[0].Resources, 1)
	assert.Equal(t, ResourceStatusLocalChanged, plans[0].Resources[0].Status)
	assert.Equal(t, "zeta", plans[1].ProjectKey)
	assert.Equal(t, ResourceStatusServerChanged, plans[1].Resources[0].Status)
}

func TestClientPlanReturnsEmptyResultWithoutResources(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport)

	plans, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeLocal, "sha256.local"),
		true,
		nil,
	)

	require.NoError(t, err)
	assert.Empty(t, plans)
	assert.Empty(t, transport.Requests)
}

func TestClientPlanRejectsUnsupportedResource(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport)

	_, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/acme/repo"),
		true,
		[]syncdomain.SyncedResource{{
			ProjectKey: "project",
			Kind:       "unknown",
			LookupKey:  "unknown",
		}},
	)

	require.ErrorContains(t, err, `unsupported sync resource kind "unknown"`)
	assert.Empty(t, transport.Requests)
}

func TestClientPlanReturnsTransportError(t *testing.T) {
	client := NewClient(&recordingClient{Err: errors.New("unavailable")})

	_, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/acme/repo"),
		true,
		[]syncdomain.SyncedResource{variationResource("project", "config/key", "Name", false)},
	)

	require.ErrorContains(t, err, "unavailable")
}

func TestClientPlanRejectsInvalidResponse(t *testing.T) {
	client := NewClient(&recordingClient{Responses: [][]byte{[]byte(`not json`)}})

	_, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/acme/repo"),
		true,
		[]syncdomain.SyncedResource{variationResource("project", "config/key", "Name", false)},
	)

	require.ErrorContains(t, err, "decode plan response")
}

func variationResource(
	projectKey string,
	lookupKey string,
	name string,
	upsert bool,
) syncdomain.SyncedResource {
	payload, err := json.Marshal(map[string]string{
		"key":  lookupKey[len("config/"):],
		"name": name,
	})
	if err != nil {
		panic(err)
	}

	return syncdomain.SyncedResource{
		ProjectKey: projectKey,
		Kind:       syncdomain.KindVariation,
		LookupKey:  lookupKey,
		Upsert:     upsert,
		Payload:    payload,
	}
}

func requireSource(
	t *testing.T,
	sourceType syncdomain.SourceType,
	identifier string,
) syncdomain.Source {
	t.Helper()

	source, err := syncdomain.NewSource(sourceType, identifier)
	require.NoError(t, err)

	return source
}
