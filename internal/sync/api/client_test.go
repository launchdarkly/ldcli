package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/api/apitest"
)

// recordingServer records each request. It sends Responses in order, or Status and ErrorBody to each request.
type recordingServer struct {
	Requests  []apitest.Request
	Responses [][]byte
	Status    int
	ErrorBody string
}

func (recorder *recordingServer) client(t *testing.T) Client {
	t.Helper()
	server := apitest.NewServer(t, func(request apitest.Request) (int, []byte) {
		recorder.Requests = append(recorder.Requests, request)
		switch {
		case recorder.Status != 0:
			return recorder.Status, []byte(recorder.ErrorBody)
		case len(recorder.Responses) < len(recorder.Requests):
			return http.StatusOK, nil
		default:
			return http.StatusOK, recorder.Responses[len(recorder.Requests)-1]
		}
	})
	return NewTransport("test").Client(Options{BaseURI: server.URL, AccessToken: "token"})
}

func TestZeroTransportSendsRequests(t *testing.T) {
	server := apitest.NewServer(t, func(apitest.Request) (int, []byte) {
		return http.StatusOK, []byte(`{"key":"support","mode":"agent","variations":[]}`)
	})

	_, err := Transport{}.Client(Options{BaseURI: server.URL}).ReadVariation("project", "support", "default")

	require.NoError(t, err)
}

func TestClientVariationReadsParentConfig(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"key": "support",
		"name": "Support agent",
		"mode": "agent",
		"variations": [
			{
				"mode": "completion",
				"key": "other",
				"name": "Other"
			},
			{
				"mode": "completion",
				"key": "helpful",
				"name": "Helpful",
				"instructions": "Help the user.",
				"model": {"modelName": "claude"}
			}
		]
	}`)}}
	client := transport.client(t)

	state, err := client.ReadVariation("project", "support", "helpful")

	require.NoError(t, err)
	assert.True(t, state.Exists)
	assert.Equal(t, syncdomain.VariationModeAgent, state.ConfigMode)
	assert.Equal(t, syncdomain.VariationModeAgent, state.Variation.Mode)
	assert.Equal(t, "helpful", state.Variation.Key)
	assert.Equal(t, "Help the user.", state.Variation.Instructions)
	assert.Equal(t, "claude", state.Variation.Model["modelName"])

	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.Header.Get("Authorization"))
	assert.Equal(t, "GET", request.Method)
	assert.Equal(
		t,
		"/api/v2/projects/project/ai-configs/support",
		request.Path,
	)
	assert.Empty(t, request.Header.Get("Content-Type"))
	assert.Empty(t, request.Query)
	assert.Empty(t, request.Body)
	assert.Equal(t, apiVersion, request.Header.Get("LD-API-Version"))
}

func TestClientVariationReturnsFalseWhenMissing(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"key": "support",
		"mode": "agent",
		"variations": [{"key": "other", "name": "Other"}]
	}`)}}
	client := transport.client(t)

	state, err := client.ReadVariation("project", "support", "missing")

	require.NoError(t, err)
	assert.False(t, state.Exists)
	assert.Equal(t, syncdomain.VariationModeAgent, state.ConfigMode)
	assert.Equal(t, syncdomain.Variation{}, state.Variation)
}

func TestClientVariationTreatsArchivedVariationAsMissing(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"key": "support",
		"mode": "agent",
		"variations": [{
			"key": "helpful",
			"name": "Helpful",
			"state": "archived",
			"instructions": "Help the user."
		}]
	}`)}}
	client := transport.client(t)

	state, err := client.ReadVariation("project", "support", "helpful")

	require.NoError(t, err)
	assert.False(t, state.Exists)
	assert.Equal(t, syncdomain.VariationModeAgent, state.ConfigMode)
	assert.Equal(t, syncdomain.Variation{}, state.Variation)
}

func TestClientVariationReturnsTransportError(t *testing.T) {
	client := (&recordingServer{Status: apitest.CloseConnection}).client(t)

	_, err := client.ReadVariation("project", "support", "helpful")

	require.ErrorContains(t, err, `get config "support" in project "project": Get "`)
}

func TestClientVariationRejectsInvalidResponse(t *testing.T) {
	client := (&recordingServer{Responses: [][]byte{[]byte(`not json`)}}).client(t)

	_, err := client.ReadVariation("project", "support", "helpful")

	require.ErrorContains(t, err, "decode config response")
}

func TestClientVariationReadsModelConfigVersion(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"key": "support",
		"mode": "agent",
		"variations": [{
			"key": "helpful",
			"name": "Helpful",
			"modelConfigVersion": 2
		}]
	}`)}}
	client := transport.client(t)

	state, err := client.ReadVariation("project", "support", "helpful")

	require.NoError(t, err)
	assert.Equal(t, 2, state.Variation.ModelConfigVersion)
}

func TestClientCreateVariation(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"key": "concise",
		"name": "Concise"
	}`)}}
	client := transport.client(t)
	variation := syncdomain.Variation{
		Mode:               syncdomain.VariationModeCompletion,
		Key:                "concise",
		Name:               "Concise",
		ModelConfigKey:     "claude",
		ModelConfigVersion: 2,
		Model: map[string]any{
			"modelName": "claude-3-5-sonnet",
			"parameters": map[string]any{
				"temperature": 0.2,
			},
		},
		Messages: []syncdomain.Message{{
			Role:    "system",
			Content: "Be concise.",
		}},
		Instructions: "stale agent instructions",
		OutputFormat: map[string]any{
			"type": "object",
		},
	}

	err := client.CreateVariation("project", "support", variation)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.Header.Get("Authorization"))
	assert.Equal(t, "POST", request.Method)
	assert.Equal(
		t,
		"/api/v2/projects/project/ai-configs/support/variations",
		request.Path,
	)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.Empty(t, request.Query)
	assert.Equal(t, apiVersion, request.Header.Get("LD-API-Version"))
	assert.JSONEq(t, `{
		"key": "concise",
		"name": "Concise",
		"modelConfigKey": "claude",
		"modelConfigVersion": 2,
		"model": {
			"modelName": "claude-3-5-sonnet",
			"parameters": {"temperature": 0.2}
		},
		"outputFormat": {"type": "object"},
		"messages": [{"role": "system", "content": "Be concise."}]
	}`, string(request.Body))
	assert.NotContains(t, string(request.Body), `"mode"`)
	assert.NotContains(t, string(request.Body), `"instructions"`)
}

func TestClientCreateAgentVariationOmitsCompletionFields(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Messages = []syncdomain.Message{{Role: "user", Content: "stale completion message"}}

	require.NoError(t, client.CreateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.Contains(t, string(transport.Requests[0].Body), `"instructions":"Help the user."`)
	assert.NotContains(t, string(transport.Requests[0].Body), `"messages"`)
}

func TestClientCreateVariationReturnsTransportError(t *testing.T) {
	client := (&recordingServer{Status: apitest.CloseConnection}).client(t)

	err := client.CreateVariation(
		"project",
		"support",
		testVariation(syncdomain.VariationModeAgent),
	)

	require.ErrorContains(t, err, `create config variation "helpful": Post "`)
	assert.True(t, MutationMayHaveSucceeded(err))
}

func TestClientMutationRecognizesDefinitiveAPIError(t *testing.T) {
	client := (&recordingServer{Status: 400, ErrorBody: `{"code":"invalid_request","statusCode":400}`}).client(t)

	err := client.CreateVariation(
		"project",
		"support",
		testVariation(syncdomain.VariationModeAgent),
	)

	require.ErrorContains(t, err, `"statusCode":400`)
	assert.False(t, MutationMayHaveSucceeded(err))
}

func TestClientCreateVariationReturnsEncodeError(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Model = map[string]any{"invalid": make(chan int)}

	err := client.CreateVariation("project", "support", variation)

	require.ErrorContains(t, err, `encode config variation "helpful"`)
	assert.Empty(t, transport.Requests)
}

func TestClientUpdateVariation(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"key": "helpful",
		"name": "Very helpful"
	}`)}}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Name = "Very helpful"
	variation.ModelConfigVersion = 3
	variation.OutputFormat = map[string]any{"type": "object"}

	err := client.UpdateVariation("project", "support", variation)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.Header.Get("Authorization"))
	assert.Equal(t, "PATCH", request.Method)
	assert.Equal(
		t,
		"/api/v2/projects/project/ai-configs/support/variations/helpful",
		request.Path,
	)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.Empty(t, request.Query)
	assert.NotEmpty(t, request.Body)
	assert.Equal(t, apiVersion, request.Header.Get("LD-API-Version"))
	assert.JSONEq(t, `{
		"name": "Very helpful",
		"instructions": "Help the user.",
		"modelConfigKey": "claude",
		"modelConfigVersion": 3,
		"model": {"modelName": "claude-3-5-sonnet"},
		"outputFormat": {"type": "object"}
	}`, string(request.Body))
	assert.NotContains(t, string(request.Body), `"key"`)
	assert.NotContains(t, string(request.Body), `"mode"`)
}

func TestClientUpdateVariationSendsEmptyOwnedFieldsToClearThem(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent,
		Key:  "helpful",
		Name: "Helpful",
	}

	require.NoError(t, client.UpdateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.JSONEq(t, `{
		"name": "Helpful",
		"instructions": "",
		"model": {},
		"outputFormat": {}
	}`, string(transport.Requests[0].Body))
}

func TestClientUpdateCompletionVariationOmitsAgentFields(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeCompletion)
	variation.Messages = []syncdomain.Message{{Role: "system", Content: "Help the user."}}

	require.NoError(t, client.UpdateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.JSONEq(t, `{
		"name": "Helpful",
		"modelConfigKey": "claude",
		"model": {"modelName": "claude-3-5-sonnet"},
		"outputFormat": {},
		"messages": [{"role": "system", "content": "Help the user."}]
	}`, string(transport.Requests[0].Body))
	assert.NotContains(t, string(transport.Requests[0].Body), `"instructions"`)
	assert.NotContains(t, string(transport.Requests[0].Body), `"description"`)
}

func TestClientUpdateCompletionVariationSendsEmptyMessagesToClearThem(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeCompletion)

	require.NoError(t, client.UpdateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.Contains(t, string(transport.Requests[0].Body), `"messages":[]`)
	assert.NotContains(t, string(transport.Requests[0].Body), `"instructions"`)
}

func TestClientUpdateVariationReturnsTransportError(t *testing.T) {
	client := (&recordingServer{Status: apitest.CloseConnection}).client(t)

	err := client.UpdateVariation(
		"project",
		"support",
		testVariation(syncdomain.VariationModeAgent),
	)

	require.ErrorContains(t, err, `update config variation "helpful": Patch "`)
}

func TestClientUpdateVariationReturnsEncodeError(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Model = map[string]any{"invalid": make(chan int)}

	err := client.UpdateVariation("project", "support", variation)

	require.ErrorContains(t, err, `encode config variation "helpful"`)
	assert.Empty(t, transport.Requests)
}

func TestClientArchiveVariation(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)

	err := client.ArchiveVariation("project", "support", "helpful")

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.Header.Get("Authorization"))
	assert.Equal(t, "PATCH", request.Method)
	assert.Equal(
		t,
		"/api/v2/projects/project/ai-configs/support/variations/helpful",
		request.Path,
	)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.Empty(t, request.Query)
	assert.JSONEq(t, `{"state":"archived"}`, string(request.Body))
	assert.Equal(t, apiVersion, request.Header.Get("LD-API-Version"))
}

func TestClientArchiveVariationReturnsTransportError(t *testing.T) {
	client := (&recordingServer{Status: apitest.CloseConnection}).client(t)

	err := client.ArchiveVariation("project", "support", "helpful")

	require.ErrorContains(t, err, `archive config variation "helpful": Patch "`)
}

func TestClientGetsSyncManifest(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"source": "git:example.com/acme/repo",
		"items": [{
			"resourceKind": "variation",
			"resourceLookupKey": "support/helpful",
			"fingerprint": "sha256:abc",
			"version": 3
		}]
	}`)}}
	client := transport.client(t)

	manifest, err := client.GetSyncManifest("project", "git:example.com/acme/repo")

	require.NoError(t, err)
	require.Len(t, manifest.Items, 1)
	assert.Equal(t, 3, manifest.Items[0].Version)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "/api/v2/projects/project/configs/sync/manifests", request.Path)
	assert.Equal(t, "git:example.com/acme/repo", request.Query.Get("source"))
}

func TestClientPatchesSyncManifest(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(`{
		"source": "git:example.com/acme/repo",
		"items": []
	}`)}}
	client := transport.client(t)

	_, err := client.PatchSyncManifest(
		"project",
		"git:example.com/acme/repo",
		[]SyncManifestUpsert{{
			ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/helpful",
			Fingerprint: "sha256:abc", Version: 2,
		}},
		nil,
	)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodPatch, request.Method)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.JSONEq(t, `{
		"source": "git:example.com/acme/repo",
		"upserts": [{
			"resourceKind": "variation",
			"resourceLookupKey": "support/helpful",
			"fingerprint": "sha256:abc",
			"version": 2
		}],
		"deletions": []
	}`, string(request.Body))
}

func TestIsConflictRecognizesAPIStatus(t *testing.T) {
	assert.True(t, IsConflict(errors.New(`{"code":"conflict","statusCode":409}`)))
	assert.False(t, IsConflict(errors.New(`{"code":"invalid_request","statusCode":400}`)))
}

func TestContextualAPIErrorPreservesResponseFields(t *testing.T) {
	err := contextualAPIError(
		errors.New(`{
			"code":"not_found",
			"message":"AI config not found",
			"statusCode":404,
			"suggestion":"generic suggestion"
		}`),
		`get config "agent" in project "default"`,
		"default",
	)

	assert.JSONEq(t, `{
		"code":"not_found",
		"message":"get config \"agent\" in project \"default\"",
		"statusCode":404,
		"suggestion":"Verify the resource key and that it belongs to project \"default\"."
	}`, err.Error())
}

func TestContextualAPIErrorPreservesSpecificMutationFailure(t *testing.T) {
	err := contextualAPIError(
		errors.New(`{
			"code":"not_found",
			"message":"model config version not found",
			"statusCode":404,
			"suggestion":"Select an existing model config version."
		}`),
		`update config variation "default"`,
		"",
	)

	assert.JSONEq(t, `{
		"code":"not_found",
		"message":"update config variation \"default\": model config version not found",
		"statusCode":404,
		"suggestion":"Select an existing model config version."
	}`, err.Error())
}

func testVariation(mode syncdomain.VariationMode) syncdomain.Variation {
	return syncdomain.Variation{
		Mode:           mode,
		Key:            "helpful",
		Name:           "Helpful",
		Instructions:   "Help the user.",
		ModelConfigKey: "claude",
		Model:          map[string]any{"modelName": "claude-3-5-sonnet"},
	}
}

func TestClientUpdateVariationOmitsEmptyModelConfigKey(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.ModelConfigKey = ""

	require.NoError(t, client.UpdateVariation("project", "support", variation))
	require.NoError(t, client.CreateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 2)
	for _, request := range transport.Requests {
		assert.NotContains(t, string(request.Body), `"modelConfigKey"`, request.Method)
	}
}

func TestClientReportsErrorResponsesAsTheResourcesClientDoes(t *testing.T) {
	tests := map[string]struct {
		status int
		body   string
		want   string
	}{
		"JSON without a status": {
			status: http.StatusBadRequest,
			body:   `{"code":"invalid_request","message":"Invalid key"}`,
			want:   `{"code":"invalid_request","message":"Invalid key","statusCode":400}`,
		},
		"text": {
			status: http.StatusBadGateway,
			body:   "upstream failed",
			want:   `{"code":"bad_gateway","message":"upstream failed","statusCode":502}`,
		},
		"empty": {
			status: http.StatusConflict,
			want: `{"code":"conflict","message":"Conflict","statusCode":409,` +
				`"suggestion":"Conflict: the resource may have been modified since you last read it. Fetch the latest version and retry."}`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			client := NewTransport("test").Client(Options{BaseURI: server.URL, AccessToken: "token"})

			_, err := client.do(http.MethodGet, server.URL+"/api/v2/projects", nil, nil)

			require.Error(t, err)
			assert.JSONEq(t, test.want, err.Error())
			status, ok := responseStatusCode(err)
			assert.True(t, ok)
			assert.Equal(t, test.status, status)
		})
	}
}
