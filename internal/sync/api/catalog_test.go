package api

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestCatalogClientSearchProjectsFiltersAndReturnsOnePage(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, Page[Project]{
			Items:      []Project{{Key: "support", Name: "Support"}},
			TotalCount: 1,
		}),
	}}

	page, err := NewClient(transport, "token", "https://example.com").SearchProjects("supp", 10, 20)

	require.NoError(t, err)
	require.Equal(t, []Project{{Key: "support", Name: "Support"}}, page.Items)
	assert.Equal(t, 1, page.TotalCount)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "query:supp", request.Query.Get("filter"))
	assert.Equal(t, "name", request.Query.Get("sort"))
	assert.Equal(t, "10", request.Query.Get("limit"))
	assert.Equal(t, "20", request.Query.Get("offset"))
}

func TestCatalogClientSearchConfigsFiltersAndAppliesMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, Page[Config]{
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
		}),
	}}

	page, err := NewClient(
		transport,
		"token",
		"https://example.com",
	).SearchConfigs("project", "support", []syncdomain.VariationMode{syncdomain.VariationModeAgent}, 10, 20)

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Len(t, page.Items[0].Variations, 1)
	assert.Equal(
		t,
		syncdomain.VariationModeAgent,
		page.Items[0].Variations[0].Mode,
	)
	assert.Equal(t, 1, page.TotalCount)

	request := transport.Requests[0]
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs",
		request.Path,
	)
	assert.Equal(t, "name", request.Query.Get("sort"))
	assert.Equal(
		t,
		`query equals "support", mode anyOf ["agent"]`,
		request.Query.Get("filter"),
	)
	assert.Equal(t, "10", request.Query.Get("limit"))
	assert.Equal(t, "20", request.Query.Get("offset"))
	assert.False(t, request.IsBeta)
}

func TestCatalogClientConfigsDefaultsCompletionMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, Page[Config]{
			Items: []Config{{
				Key:        "completion",
				Name:       "Completion",
				Variations: []syncdomain.Variation{{Key: "strict", Name: "Strict"}},
			}},
			TotalCount: 1,
		}),
	}}

	page, err := NewClient(
		transport,
		"token",
		"https://example.com",
	).SearchConfigs("project", "", nil, 25, 0)

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Len(t, page.Items[0].Variations, 1)
	assert.Equal(
		t,
		syncdomain.VariationModeCompletion,
		page.Items[0].Variations[0].Mode,
	)
	assert.Equal(t, `mode anyOf ["agent","completion"]`, transport.Requests[0].Query.Get("filter"))
}

func TestCatalogClientConfigsRejectsJudgeMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, Page[Config]{
			Items: []Config{{
				Key:  "config",
				Name: "Config",
				Mode: "judge",
			}},
			TotalCount: 1,
		}),
	}}

	_, err := NewClient(
		transport,
		"token",
		"https://example.com",
	).SearchConfigs("project", "", nil, 25, 0)

	require.ErrorContains(t, err, `unsupported mode "judge"`)
}

func TestCatalogClientConfigGetsExactConfigAndAppliesMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, Config{
			Key:  "support",
			Name: "Support agent",
			Mode: syncdomain.VariationModeAgent,
			Variations: []syncdomain.Variation{{
				Key:          "helpful",
				Name:         "Helpful",
				Instructions: "Help the user.",
			}},
		}),
	}}

	config, err := NewClient(
		transport,
		"token",
		"https://example.com",
	).Config("project", "support")

	require.NoError(t, err)
	require.Len(t, config.Variations, 1)
	assert.Equal(t, syncdomain.VariationModeAgent, config.Variations[0].Mode)

	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "GET", request.Method)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/support",
		request.Path,
	)
	assert.Empty(t, request.Query)
	assert.False(t, request.IsBeta)
}

func TestCatalogClientConfigRejectsInvalidResponse(t *testing.T) {
	client := NewClient(
		&recordingClient{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.Config("project", "support")

	require.ErrorContains(t, err, "decode config response")
}

func TestCatalogClientSearchProjectsRejectsInvalidResponse(t *testing.T) {
	client := NewClient(
		&recordingClient{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.SearchProjects("", 25, 0)

	require.ErrorContains(t, err, "decode projects response")
}

func TestCatalogClientModelConfigsGetsBareArray(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`[
		{
			"key":"claude-sonnet",
			"id":"claude-3-5-sonnet-20241022",
			"name":"Claude Sonnet",
			"version":4,
			"provider":"anthropic",
			"params":{"temperature":0.2},
			"customParams":{"region":"us-east"}
		},
		{"key":"gpt-5","id":"gpt-5-2025-08-07","name":"GPT-5","provider":"openai"}
	]`)}}

	modelConfigs, err := NewClient(
		transport,
		"token",
		"https://example.com",
	).ModelConfigs("project")

	require.NoError(t, err)
	assert.Equal(t, []ModelConfig{
		{
			Key: "claude-sonnet", ID: "claude-3-5-sonnet-20241022", Name: "Claude Sonnet", Version: 4,
			Params: map[string]any{"temperature": 0.2}, CustomParams: map[string]any{"region": "us-east"},
		},
		{Key: "gpt-5", ID: "gpt-5-2025-08-07", Name: "GPT-5"},
	}, modelConfigs)
	assert.Equal(t, map[string]any{
		"modelName":  "claude-3-5-sonnet-20241022",
		"parameters": map[string]any{"temperature": 0.2},
		"custom":     map[string]any{"region": "us-east"},
	}, modelConfigs[0].VariationModel())
	assert.Equal(t, map[string]any{
		"modelName": "gpt-5-2025-08-07", "parameters": map[string]any{}, "custom": map[string]any{},
	}, modelConfigs[1].VariationModel())

	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, "GET", request.Method)
	assert.Equal(t, "token", request.AccessToken)
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs/model-configs",
		request.Path,
	)
	assert.Empty(t, request.Query)
	assert.False(t, request.IsBeta)
}

func TestCatalogClientModelConfigsReturnsRequestError(t *testing.T) {
	client := NewClient(
		&recordingClient{Err: errors.New("unavailable")},
		"token",
		"https://example.com",
	)

	_, err := client.ModelConfigs("project")

	require.ErrorContains(t, err, `list model configs in project "project": unavailable`)
}

func TestCatalogClientModelConfigsRejectsInvalidResponse(t *testing.T) {
	client := NewClient(
		&recordingClient{Responses: [][]byte{[]byte(`{"items":[]}`)}},
		"token",
		"https://example.com",
	)

	_, err := client.ModelConfigs("project")

	require.ErrorContains(t, err, "decode model configs response")
}

func mustCatalogJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)

	return data
}
