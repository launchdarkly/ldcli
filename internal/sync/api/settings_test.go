package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestSyncDirections(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{
		"items": [
			{"resourceKind":"variation","resourceLookupKey":"support/default","direction":"code_canonical"},
			{"resourceKind":"tool","resourceLookupKey":"search","direction":"server_canonical"}
		]
	}`)}}
	client := NewClient(transport, "token", "https://example.com")
	variation := syncdomain.VariationID("project", "support", "default")
	tool := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}

	directions, err := client.SyncDirections("project", []syncdomain.ResourceID{variation, tool})
	require.NoError(t, err)
	assert.Equal(t, syncdomain.SyncDirectionCodeCanonical, directions[variation])
	assert.Equal(t, syncdomain.SyncDirectionServerCanonical, directions[tool])
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, "https://example.com/api/v2/projects/project/configs/sync/settings", request.Path)
	assert.Equal(t, []string{"variation:support/default", "tool:search"}, request.Query["resources"])
}

func TestSyncDirectionsReturnsOnlyResolvedResults(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{[]byte(`{"items":[]}`)}}
	client := NewClient(transport, "token", "https://example.com")
	id := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}

	directions, err := client.SyncDirections("project", []syncdomain.ResourceID{id})

	require.NoError(t, err)
	assert.Empty(t, directions)
}
