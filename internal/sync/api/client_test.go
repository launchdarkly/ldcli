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

type apiRequest struct {
	AccessToken string
	Method      string
	Path        string
	Query       url.Values
	Body        []byte
	IsBeta      bool
}

type apiClientStub struct {
	Requests  []apiRequest
	Responses [][]byte
	Err       error
}

var _ resources.Client = &apiClientStub{}

func (c *apiClientStub) MakeRequest(
	accessToken string,
	method string,
	path string,
	_ string,
	query url.Values,
	body []byte,
	isBeta bool,
) ([]byte, error) {
	c.Requests = append(c.Requests, apiRequest{
		AccessToken: accessToken,
		Method:      method,
		Path:        path,
		Query:       query,
		Body:        append([]byte(nil), body...),
		IsBeta:      isBeta,
	})
	if c.Err != nil {
		return nil, c.Err
	}

	return c.Responses[len(c.Requests)-1], nil
}

func (*apiClientStub) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestAPIClient_ProjectsPaginatesInNameOrder(t *testing.T) {
	firstPage := make([]Project, listPageLimit)
	for index := range firstPage {
		firstPage[index] = Project{
			Key:  string(rune('a' + index)),
			Name: string(rune('A' + index)),
		}
	}

	transport := &apiClientStub{Responses: [][]byte{
		mustJSON(t, listResponse[Project]{Items: firstPage, TotalCount: 26}),
		mustJSON(t, listResponse[Project]{
			Items:      []Project{{Key: "z", Name: "Z"}},
			TotalCount: 26,
		}),
	}}

	projects, err := NewAPIClient(
		transport,
		"token",
		"https://example.com",
	).Projects("my-project")
	require.NoError(t, err)
	require.Len(t, projects, 26)
	require.Len(t, transport.Requests, 2)

	assert.Equal(t, "GET", transport.Requests[0].Method)
	assert.Equal(t, "https://example.com/api/v2/projects", transport.Requests[0].Path)
	assert.Equal(t, "name", transport.Requests[0].Query.Get("sort"))
	assert.Equal(t, "query:my-project", transport.Requests[0].Query.Get("filter"))
	assert.Equal(t, "25", transport.Requests[0].Query.Get("limit"))
	assert.Equal(t, "0", transport.Requests[0].Query.Get("offset"))
	assert.Equal(t, "25", transport.Requests[1].Query.Get("offset"))
	assert.Equal(t, "query:my-project", transport.Requests[1].Query.Get("filter"))
	assert.Equal(t, "Z", projects[25].Name)
}

func TestAPIClient_ConfigsPaginatesAndAppliesMode(t *testing.T) {
	transport := &apiClientStub{Responses: [][]byte{mustJSON(t, listResponse[Config]{
		Items: []Config{{
			Key:  "support",
			Name: "Support agent",
			Mode: syncdomain.VariationModeAgent,
			Variations: []syncdomain.Variation{{
				Key:  "helpful",
				Name: "Helpful",
			}},
		}},
		TotalCount: 1,
	})}}

	configs, err := NewAPIClient(
		transport,
		"token",
		"https://example.com",
	).Configs(
		"project",
		"customer support",
	)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Len(t, configs[0].Variations, 1)

	request := transport.Requests[0]
	assert.Equal(t, "https://example.com/api/v2/projects/project/ai-configs", request.Path)
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(t, "name", request.Query.Get("sort"))
	assert.Equal(t, "25", request.Query.Get("limit"))
	assert.Equal(
		t,
		`query equals "customer support",mode anyOf ["agent","completion"]`,
		request.Query.Get("filter"),
	)
	assert.True(t, request.IsBeta)
	assert.Equal(t, syncdomain.VariationModeAgent, configs[0].Variations[0].Mode)
}

func TestAPIClient_ConfigsFetchesEveryPage(t *testing.T) {
	firstPage := make([]Config, listPageLimit)
	for index := range firstPage {
		firstPage[index] = Config{
			Key:  string(rune('a' + index)),
			Name: string(rune('A' + index)),
			Mode: syncdomain.VariationModeCompletion,
		}
	}
	transport := &apiClientStub{Responses: [][]byte{
		mustJSON(t, listResponse[Config]{Items: firstPage, TotalCount: 26}),
		mustJSON(t, listResponse[Config]{
			Items: []Config{{
				Key:  "z",
				Name: "Z",
				Mode: syncdomain.VariationModeAgent,
			}},
			TotalCount: 26,
		}),
	}}

	configs, err := NewAPIClient(
		transport,
		"token",
		"https://example.com",
	).Configs(
		"project",
		"",
	)
	require.NoError(t, err)
	require.Len(t, configs, 26)
	require.Len(t, transport.Requests, 2)
	assert.Equal(t, "0", transport.Requests[0].Query.Get("offset"))
	assert.Equal(t, "25", transport.Requests[1].Query.Get("offset"))
	assert.Equal(t, syncdomain.VariationModeAgent, configs[25].Mode)
	assert.Equal(
		t,
		`mode anyOf ["agent","completion"]`,
		transport.Requests[1].Query.Get("filter"),
	)
	assert.True(t, transport.Requests[1].IsBeta)
}

func TestAPIClient_ConfigGetsCurrentVariations(t *testing.T) {
	transport := &apiClientStub{Responses: [][]byte{[]byte(`{
		"key": "completion",
		"name": "Completion",
		"mode": "completion",
		"variations": [{"key": "strict", "name": "Strict"}]
	}`)}}

	config, err := NewAPIClient(
		transport,
		"token",
		"https://example.com",
	).Config(
		"project",
		"completion",
	)
	require.NoError(t, err)
	assert.Equal(t, syncdomain.VariationModeCompletion, config.Variations[0].Mode)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/completion",
		transport.Requests[0].Path,
	)
	assert.True(t, transport.Requests[0].IsBeta)
}

func TestAPIClient_ConfigRejectsUnsupportedMode(t *testing.T) {
	transport := &apiClientStub{Responses: [][]byte{[]byte(
		`{"key":"config","name":"Config","mode":"unknown","variations":[]}`,
	)}}

	_, err := NewAPIClient(
		transport,
		"token",
		"https://example.com",
	).Config(
		"project",
		"config",
	)
	require.ErrorContains(t, err, `unsupported mode "unknown"`)
}

func TestAPIClient_ProjectsInvalidResponse(t *testing.T) {
	client := NewAPIClient(
		&apiClientStub{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.Projects("")
	require.ErrorContains(t, err, "decode projects response")
}

func TestAPIClient_Status(t *testing.T) {
	transport := &apiClientStub{
		Responses: [][]byte{
			[]byte(`[{"resourceKind":"variation","lookupKey":"config/first","status":"local_changed","syncDirection":"code_canonical"}]`),
			[]byte(`[{"resourceKind":"tool","lookupKey":"search/2","status":"in_sync","syncDirection":"both"}]`),
		},
	}
	client := NewAPIClient(transport, "token", "https://example.com")

	statuses, err := client.Status(
		"launchdarkly/ldcli",
		[]syncdomain.SyncedResource{
			{
				ProjectKey:  "alpha",
				Kind:        syncdomain.KindVariation,
				LookupKey:   "config/first",
				Fingerprint: "sha256.first",
				Upsert:      true,
			},
			{
				ProjectKey:  "zeta",
				Kind:        syncdomain.KindTool,
				LookupKey:   "search/2",
				Fingerprint: "sha256.second",
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, transport.Requests, 2)
	require.Len(t, statuses, 2)

	assert.Equal(t, "POST", transport.Requests[0].Method)
	assert.Equal(t, "token", transport.Requests[0].AccessToken)
	assert.Equal(t, "https://example.com/api/v2/projects/alpha/ai-configs/sync/status", transport.Requests[0].Path)
	assert.Equal(t, "alpha", statuses[0].ProjectKey)
	assert.Equal(t, "zeta", statuses[1].ProjectKey)

	var request statusRequest
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &request))
	assert.Equal(t, "launchdarkly/ldcli", request.RepoIdentifier)
	require.Len(t, request.Resources, 1)
	assert.Equal(t, syncdomain.KindVariation, request.Resources[0].ResourceKind)
	assert.Equal(t, "config/first", request.Resources[0].LookupKey)
	assert.Equal(t, syncdomain.Fingerprint("sha256.first"), request.Resources[0].Fingerprint)
	assert.True(t, request.Resources[0].Upsert)
}

func TestAPIClient_StatusTransportError(t *testing.T) {
	client := NewAPIClient(
		&apiClientStub{Err: errors.New("unavailable")},
		"token",
		"https://example.com",
	)

	_, err := client.Status(
		"launchdarkly/ldcli",
		[]syncdomain.SyncedResource{{ProjectKey: "proj"}},
	)
	require.ErrorContains(t, err, "unavailable")
}

func TestAPIClient_StatusInvalidResponse(t *testing.T) {
	client := NewAPIClient(
		&apiClientStub{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.Status(
		"launchdarkly/ldcli",
		[]syncdomain.SyncedResource{{ProjectKey: "proj"}},
	)
	require.ErrorContains(t, err, "decode status response")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}
