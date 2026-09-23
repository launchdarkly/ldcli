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
					"manifestUpdateRequired": true,
					"localDeleted": true,
					"serverDeleted": false,
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
		nil,
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
	assert.True(t, request.FullInventory)
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
	assert.True(t, plans[0].Resources[0].ManifestUpdateRequired)
	assert.True(t, plans[0].Resources[0].LocalDeleted)
	assert.False(t, plans[0].Resources[0].ServerDeleted)
	assert.Equal(t, "zeta", plans[1].ProjectKey)
	assert.Equal(t, ResourceStatusServerChanged, plans[1].Resources[0].Status)
}

func TestClientPlanDecodesDurablePlanIdentity(t *testing.T) {
	transport := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"planId": "617c83f1-cd9a-4865-8f37-bb11f88e2147",
			"expiresAt": "2026-12-14T12:00:00Z",
			"resources": []
		}`)},
	}
	client := NewClient(transport)

	plans, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/launchdarkly/example"),
		false,
		nil,
		[]syncdomain.SyncedResource{
			variationResource("project", "config/first", "First", true),
		},
	)

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, "project", plans[0].ProjectKey)
	assert.Equal(t, "617c83f1-cd9a-4865-8f37-bb11f88e2147", plans[0].PlanID)
	assert.Equal(t, "2026-12-14T12:00:00Z", plans[0].ExpiresAt)

	var request planRequest
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &request))
	assert.False(t, request.DryRun)
}

func TestClientPlanReturnsEmptyResultWithoutResources(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport)

	plans, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/launchdarkly/example"),
		true,
		nil,
		nil,
	)

	require.NoError(t, err)
	assert.Empty(t, plans)
	assert.Empty(t, transport.Requests)
}

func TestClientPlanSendsEmptyProjectInventory(t *testing.T) {
	transport := &recordingClient{
		Responses: [][]byte{[]byte(`{"resources":[]}`)},
	}
	client := NewClient(transport)

	plans, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/launchdarkly/example"),
		true,
		[]string{"project"},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, "project", plans[0].ProjectKey)
	require.Len(t, transport.Requests, 1)
	var request planRequest
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &request))
	assert.True(t, request.FullInventory)
	assert.Empty(t, request.Resources)
}

func TestClientPlanRejectsDurableResponseWithoutIdentity(t *testing.T) {
	client := NewClient(&recordingClient{
		Responses: [][]byte{[]byte(`{"resources":[]}`)},
	})

	_, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/acme/repo"),
		false,
		nil,
		[]syncdomain.SyncedResource{
			variationResource("project", "config/key", "Name", false),
		},
	)

	require.ErrorContains(t, err, "durable plan requires planId and expiresAt")
}

func TestClientPlanAcceptsConflictWithoutDurableIdentity(t *testing.T) {
	client := NewClient(&recordingClient{
		Responses: [][]byte{[]byte(`{
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "config/key",
				"status": "conflict",
				"syncDirection": "both"
			}]
		}`)},
	})

	plans, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/acme/repo"),
		false,
		nil,
		[]syncdomain.SyncedResource{
			variationResource("project", "config/key", "Name", false),
		},
	)

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Empty(t, plans[0].PlanID)
	assert.Empty(t, plans[0].ExpiresAt)
	assert.Equal(t, ResourceStatusConflict, plans[0].Resources[0].Status)
}

func TestClientPlanRejectsUnsupportedResource(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport)

	_, err := client.Plan(
		"token",
		"https://example.com",
		requireSource(t, syncdomain.SourceTypeGit, "github.com/acme/repo"),
		true,
		nil,
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
		nil,
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
		nil,
		[]syncdomain.SyncedResource{variationResource("project", "config/key", "Name", false)},
	)

	require.ErrorContains(t, err, "decode plan response")
}

func TestClientApply(t *testing.T) {
	transport := &recordingClient{
		Responses: [][]byte{[]byte(`{
			"planId": "617c83f1-cd9a-4865-8f37-bb11f88e2147",
			"status": "applied",
			"resources": [{
				"resourceKind": "variation",
				"lookupKey": "config/first",
				"outcome": "applied"
			}]
		}`)},
	}
	client := NewClient(transport)

	result, err := client.Apply(
		"token",
		"https://example.com",
		"project",
		"617c83f1-cd9a-4865-8f37-bb11f88e2147",
	)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "POST", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/sync/apply",
		request.Path,
	)
	assert.Equal(t, "application/json", request.ContentType)
	var body applyRequest
	require.NoError(t, json.Unmarshal(request.Body, &body))
	assert.Equal(t, "617c83f1-cd9a-4865-8f37-bb11f88e2147", body.PlanID)
	assert.JSONEq(
		t,
		`{"planId":"617c83f1-cd9a-4865-8f37-bb11f88e2147"}`,
		string(request.Body),
	)
	assert.Equal(t, "project", result.ProjectKey)
	assert.Equal(t, PlanStatusApplied, result.Status)
	require.Len(t, result.Resources, 1)
	assert.Equal(t, ResourceApplyOutcomeApplied, result.Resources[0].Outcome)
}

func TestClientApplyRejectsInvalidResponse(t *testing.T) {
	client := NewClient(&recordingClient{
		Responses: [][]byte{[]byte(`{"resources":[]}`)},
	})

	_, err := client.Apply(
		"token",
		"https://example.com",
		"project",
		"617c83f1-cd9a-4865-8f37-bb11f88e2147",
	)

	require.ErrorContains(t, err, "planId and status are required")
}

func TestClientApplyReturnsTransportError(t *testing.T) {
	client := NewClient(&recordingClient{
		Err: errors.New("sync plan has expired"),
	})

	_, err := client.Apply(
		"token",
		"https://example.com",
		"project",
		"617c83f1-cd9a-4865-8f37-bb11f88e2147",
	)

	require.ErrorContains(t, err, "sync plan has expired")
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
