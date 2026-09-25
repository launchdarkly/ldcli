package api

import (
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
	if len(client.Responses) < len(client.Requests) {
		return nil, nil
	}

	return client.Responses[len(client.Requests)-1], nil
}

func (*recordingClient) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}

func TestClientVariationReadsParentConfig(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
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
	client := NewClient(transport, "token", "https://example.com")

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
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(t, "GET", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/support",
		request.Path,
	)
	assert.Empty(t, request.ContentType)
	assert.Empty(t, request.Query)
	assert.Empty(t, request.Body)
	assert.False(t, request.IsBeta)
}

func TestClientVariationReturnsFalseWhenMissing(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "support",
		"mode": "agent",
		"variations": [{"key": "other", "name": "Other"}]
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	state, err := client.ReadVariation("project", "support", "missing")

	require.NoError(t, err)
	assert.False(t, state.Exists)
	assert.Equal(t, syncdomain.VariationModeAgent, state.ConfigMode)
	assert.Equal(t, syncdomain.Variation{}, state.Variation)
}

func TestClientVariationTreatsArchivedVariationAsMissing(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "support",
		"mode": "agent",
		"variations": [{
			"key": "helpful",
			"name": "Helpful",
			"state": "archived",
			"instructions": "Help the user."
		}]
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	state, err := client.ReadVariation("project", "support", "helpful")

	require.NoError(t, err)
	assert.False(t, state.Exists)
	assert.Equal(t, syncdomain.VariationModeAgent, state.ConfigMode)
	assert.Equal(t, syncdomain.Variation{}, state.Variation)
}

func TestClientVariationReturnsTransportError(t *testing.T) {
	client := NewClient(
		&recordingClient{Err: errors.New("unavailable")},
		"token",
		"https://example.com",
	)

	_, err := client.ReadVariation("project", "support", "helpful")

	require.ErrorContains(t, err, `get config "support": unavailable`)
}

func TestClientVariationRejectsInvalidResponse(t *testing.T) {
	client := NewClient(
		&recordingClient{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.ReadVariation("project", "support", "helpful")

	require.ErrorContains(t, err, "decode config response")
}

func TestClientVariationReadsModelConfigVersion(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "support",
		"mode": "agent",
		"variations": [{
			"key": "helpful",
			"name": "Helpful",
			"modelConfigVersion": 2
		}]
	}`)}}
	client := NewClient(transport, "token", "https://example.com")

	state, err := client.ReadVariation("project", "support", "helpful")

	require.NoError(t, err)
	assert.Equal(t, 2, state.Variation.ModelConfigVersion)
}

func TestClientCreateVariation(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "concise",
		"name": "Concise"
	}`)}}
	client := NewClient(transport, "token", "https://example.com")
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
	}

	err := client.CreateVariation("project", "support", variation)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(t, "POST", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/support/variations",
		request.Path,
	)
	assert.Equal(t, "application/json", request.ContentType)
	assert.Empty(t, request.Query)
	assert.False(t, request.IsBeta)
	assert.JSONEq(t, `{
		"key": "concise",
		"name": "Concise",
		"modelConfigKey": "claude",
		"modelConfigVersion": 2,
		"model": {
			"modelName": "claude-3-5-sonnet",
			"parameters": {"temperature": 0.2}
		},
		"messages": [{"role": "system", "content": "Be concise."}]
	}`, string(request.Body))
	assert.NotContains(t, string(request.Body), `"mode"`)
	assert.NotContains(t, string(request.Body), `"outputFormat"`)
	assert.NotContains(t, string(request.Body), `"instructions"`)
}

func TestClientCreateAgentVariationOmitsCompletionFields(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Messages = []syncdomain.Message{{Role: "user", Content: "stale completion message"}}

	require.NoError(t, client.CreateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.Contains(t, string(transport.Requests[0].Body), `"instructions":"Help the user."`)
	assert.NotContains(t, string(transport.Requests[0].Body), `"messages"`)
}

func TestClientCreateVariationReturnsTransportError(t *testing.T) {
	client := NewClient(
		&recordingClient{Err: errors.New("forbidden")},
		"token",
		"https://example.com",
	)

	err := client.CreateVariation(
		"project",
		"support",
		testVariation(syncdomain.VariationModeAgent),
	)

	require.ErrorContains(t, err, `create config variation "helpful": forbidden`)
	assert.True(t, MutationMayHaveSucceeded(err))
}

func TestClientMutationRecognizesDefinitiveAPIError(t *testing.T) {
	client := NewClient(
		&recordingClient{Err: errors.New(`{"code":"invalid_request","statusCode":400}`)},
		"token",
		"https://example.com",
	)

	err := client.CreateVariation(
		"project",
		"support",
		testVariation(syncdomain.VariationModeAgent),
	)

	require.ErrorContains(t, err, `"statusCode":400`)
	assert.False(t, MutationMayHaveSucceeded(err))
}

func TestClientCreateVariationRejectsUnsupportedDirectAPIFields(t *testing.T) {
	tests := map[string]syncdomain.Variation{
		"output format": func() syncdomain.Variation {
			variation := testVariation(syncdomain.VariationModeAgent)
			variation.OutputFormat = map[string]any{"type": "object"}
			return variation
		}(),
	}

	for name, variation := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &recordingClient{}
			client := NewClient(transport, "token", "https://example.com")

			err := client.CreateVariation("project", "support", variation)

			require.Error(t, err)
			assert.Empty(t, transport.Requests)
		})
	}
}

func TestClientCreateVariationReturnsEncodeError(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Model = map[string]any{"invalid": make(chan int)}

	err := client.CreateVariation("project", "support", variation)

	require.ErrorContains(t, err, `encode config variation "helpful"`)
	assert.Empty(t, transport.Requests)
}

func TestClientUpdateVariation(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"key": "helpful",
		"name": "Very helpful"
	}`)}}
	client := NewClient(transport, "token", "https://example.com")
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Name = "Very helpful"
	variation.ModelConfigVersion = 3

	err := client.UpdateVariation("project", "support", variation)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(t, "PATCH", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/support/variations/helpful",
		request.Path,
	)
	assert.Equal(t, "application/json", request.ContentType)
	assert.Empty(t, request.Query)
	assert.NotEmpty(t, request.Body)
	assert.False(t, request.IsBeta)
	assert.JSONEq(t, `{
		"name": "Very helpful",
		"instructions": "Help the user.",
		"modelConfigKey": "claude",
		"modelConfigVersion": 3,
		"model": {"modelName": "claude-3-5-sonnet"}
	}`, string(request.Body))
	assert.NotContains(t, string(request.Body), `"key"`)
	assert.NotContains(t, string(request.Body), `"mode"`)
	assert.NotContains(t, string(request.Body), `"outputFormat"`)
}

func TestClientUpdateVariationSendsEmptyOwnedFieldsToClearThem(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
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
		"modelConfigKey": "",
		"model": {}
	}`, string(transport.Requests[0].Body))
}

func TestClientUpdateCompletionVariationOmitsAgentFields(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
	variation := testVariation(syncdomain.VariationModeCompletion)
	variation.Messages = []syncdomain.Message{{Role: "system", Content: "Help the user."}}

	require.NoError(t, client.UpdateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.JSONEq(t, `{
		"name": "Helpful",
		"modelConfigKey": "claude",
		"model": {"modelName": "claude-3-5-sonnet"},
		"messages": [{"role": "system", "content": "Help the user."}]
	}`, string(transport.Requests[0].Body))
	assert.NotContains(t, string(transport.Requests[0].Body), `"instructions"`)
	assert.NotContains(t, string(transport.Requests[0].Body), `"description"`)
}

func TestClientUpdateCompletionVariationSendsEmptyMessagesToClearThem(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
	variation := testVariation(syncdomain.VariationModeCompletion)

	require.NoError(t, client.UpdateVariation("project", "support", variation))

	require.Len(t, transport.Requests, 1)
	assert.Contains(t, string(transport.Requests[0].Body), `"messages":[]`)
	assert.NotContains(t, string(transport.Requests[0].Body), `"instructions"`)
}

func TestClientUpdateVariationReturnsTransportError(t *testing.T) {
	client := NewClient(
		&recordingClient{Err: errors.New("conflict")},
		"token",
		"https://example.com",
	)

	err := client.UpdateVariation(
		"project",
		"support",
		testVariation(syncdomain.VariationModeAgent),
	)

	require.ErrorContains(t, err, `update config variation "helpful": conflict`)
}

func TestClientUpdateVariationRejectsUnsupportedDirectAPIFields(t *testing.T) {
	tests := map[string]syncdomain.Variation{
		"output format": func() syncdomain.Variation {
			variation := testVariation(syncdomain.VariationModeAgent)
			variation.OutputFormat = map[string]any{"type": "object"}
			return variation
		}(),
	}

	for name, variation := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &recordingClient{}
			client := NewClient(transport, "token", "https://example.com")

			err := client.UpdateVariation("project", "support", variation)

			require.Error(t, err)
			assert.Empty(t, transport.Requests)
		})
	}
}

func TestClientUpdateVariationReturnsEncodeError(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")
	variation := testVariation(syncdomain.VariationModeAgent)
	variation.Model = map[string]any{"invalid": make(chan int)}

	err := client.UpdateVariation("project", "support", variation)

	require.ErrorContains(t, err, `encode config variation "helpful"`)
	assert.Empty(t, transport.Requests)
}

func TestClientArchiveVariation(t *testing.T) {
	transport := &recordingClient{}
	client := NewClient(transport, "token", "https://example.com")

	err := client.ArchiveVariation("project", "support", "helpful")

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(t, "PATCH", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/support/variations/helpful",
		request.Path,
	)
	assert.Equal(t, "application/json", request.ContentType)
	assert.Empty(t, request.Query)
	assert.JSONEq(t, `{"state":"archived"}`, string(request.Body))
	assert.False(t, request.IsBeta)
}

func TestClientArchiveVariationReturnsTransportError(t *testing.T) {
	client := NewClient(
		&recordingClient{Err: errors.New("in use")},
		"token",
		"https://example.com",
	)

	err := client.ArchiveVariation("project", "support", "helpful")

	require.ErrorContains(t, err, `archive config variation "helpful": in use`)
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
