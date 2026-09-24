package api

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestCatalogClientProjectsPaginatesInNameOrder(t *testing.T) {
	firstPage := make([]Project, catalogPageLimit)
	for index := range firstPage {
		firstPage[index] = Project{
			Key:  string(rune('a' + index)),
			Name: string(rune('A' + index)),
		}
	}

	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, catalogPage[Project]{
			Items:      firstPage,
			TotalCount: 26,
		}),
		mustCatalogJSON(t, catalogPage[Project]{
			Items:      []Project{{Key: "z", Name: "Z"}},
			TotalCount: 26,
		}),
	}}

	projects, err := NewCatalogClient(
		transport,
		"token",
		"https://example.com",
	).Projects()

	require.NoError(t, err)
	require.Len(t, projects, 26)
	require.Len(t, transport.Requests, 2)

	firstRequest := transport.Requests[0]
	assert.Equal(t, "GET", firstRequest.Method)
	assert.Equal(t, "token", firstRequest.AccessToken)
	assert.Equal(t, "https://example.com/api/v2/projects", firstRequest.Path)
	assert.Equal(t, "name", firstRequest.Query.Get("sort"))
	assert.Equal(t, "25", firstRequest.Query.Get("limit"))
	assert.Equal(t, "0", firstRequest.Query.Get("offset"))
	assert.False(t, firstRequest.IsBeta)
	assert.Equal(t, "25", transport.Requests[1].Query.Get("offset"))
	assert.Equal(t, "Z", projects[25].Name)
}

func TestCatalogClientConfigsPaginatesAndAppliesMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, catalogPage[Config]{
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

	configs, err := NewCatalogClient(
		transport,
		"token",
		"https://example.com",
	).Configs("project")

	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Len(t, configs[0].Variations, 1)
	assert.Equal(
		t,
		syncdomain.VariationModeAgent,
		configs[0].Variations[0].Mode,
	)

	request := transport.Requests[0]
	assert.Equal(
		t,
		"https://example.com/api/v2/projects/project/ai-configs",
		request.Path,
	)
	assert.Equal(t, "name", request.Query.Get("sort"))
	assert.Equal(
		t,
		`mode anyOf ["agent","completion"]`,
		request.Query.Get("filter"),
	)
	assert.False(t, request.IsBeta)
}

func TestCatalogClientConfigsDefaultsCompletionMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, catalogPage[Config]{
			Items: []Config{{
				Key:        "completion",
				Name:       "Completion",
				Variations: []syncdomain.Variation{{Key: "strict", Name: "Strict"}},
			}},
			TotalCount: 1,
		}),
	}}

	configs, err := NewCatalogClient(
		transport,
		"token",
		"https://example.com",
	).Configs("project")

	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Len(t, configs[0].Variations, 1)
	assert.Equal(
		t,
		syncdomain.VariationModeCompletion,
		configs[0].Variations[0].Mode,
	)
}

func TestCatalogClientConfigsRejectsJudgeMode(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		mustCatalogJSON(t, catalogPage[Config]{
			Items: []Config{{
				Key:  "config",
				Name: "Config",
				Mode: "judge",
			}},
			TotalCount: 1,
		}),
	}}

	_, err := NewCatalogClient(
		transport,
		"token",
		"https://example.com",
	).Configs("project")

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

	config, err := NewCatalogClient(
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
	client := NewCatalogClient(
		&recordingClient{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.Config("project", "support")

	require.ErrorContains(t, err, "decode config response")
}

func TestCatalogClientProjectsRejectsInvalidResponse(t *testing.T) {
	client := NewCatalogClient(
		&recordingClient{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.Projects()

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

	modelConfigs, err := NewCatalogClient(
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
	client := NewCatalogClient(
		&recordingClient{Err: errors.New("unavailable")},
		"token",
		"https://example.com",
	)

	_, err := client.ModelConfigs("project")

	require.ErrorContains(t, err, "list model configs: unavailable")
}

func TestCatalogClientModelConfigsRejectsInvalidResponse(t *testing.T) {
	client := NewCatalogClient(
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
