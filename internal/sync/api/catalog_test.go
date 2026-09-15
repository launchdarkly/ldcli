package api

import (
	"encoding/json"
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

func TestCatalogClientProjectsRejectsInvalidResponse(t *testing.T) {
	client := NewCatalogClient(
		&recordingClient{Responses: [][]byte{[]byte(`not json`)}},
		"token",
		"https://example.com",
	)

	_, err := client.Projects()

	require.ErrorContains(t, err, "decode projects response")
}

func mustCatalogJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)

	return data
}
