package api

import (
	"fmt"
	"net/http"
	"strings"
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

func TestSyncDirectionsReadsEveryPage(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		[]byte(`{"items": [
			{"resourceKind":"tool","resourceLookupKey":"first","direction":"both"},
			{"resourceKind":"tool","resourceLookupKey":"second","direction":"both"}
		], "totalCount": 3}`),
		[]byte(`{"items": [
			{"resourceKind":"tool","resourceLookupKey":"third","direction":"code_canonical"}
		], "totalCount": 3}`),
	}}
	client := NewClient(transport, "token", "https://example.com")
	ids := make([]syncdomain.ResourceID, 0, 3)
	for _, key := range []string{"first", "second", "third"} {
		ids = append(ids, syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: key})
	}

	directions, err := client.SyncDirections("project", ids)

	require.NoError(t, err)
	assert.Len(t, directions, 3)
	assert.Equal(t, syncdomain.SyncDirectionCodeCanonical, directions[ids[2]])
	require.Len(t, transport.Requests, 2)
	assert.Equal(t, []string{"0"}, transport.Requests[0].Query["offset"])
	assert.Equal(t, []string{"2"}, transport.Requests[1].Query["offset"])
	assert.Equal(t, []string{"100"}, transport.Requests[1].Query["limit"])
	assert.Equal(t, []string{"tool:first", "tool:second", "tool:third"}, transport.Requests[1].Query["resources"])
}

func TestSyncDirectionsPagesWithoutATotalCount(t *testing.T) {
	full := make([]string, 0, syncSettingsPageLimit)
	ids := make([]syncdomain.ResourceID, 0, syncSettingsPageLimit)
	for index := range syncSettingsPageLimit {
		key := fmt.Sprintf("tool%d", index)
		full = append(full, fmt.Sprintf(`{"resourceKind":"tool","resourceLookupKey":%q,"direction":"both"}`, key))
		ids = append(ids, syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: key})
	}
	transport := &recordingClient{Responses: [][]byte{
		[]byte(`{"items": [` + strings.Join(full, ",") + `]}`),
		[]byte(`{"items": []}`),
	}}
	client := NewClient(transport, "token", "https://example.com")

	directions, err := client.SyncDirections("project", ids)

	require.NoError(t, err)
	assert.Len(t, directions, syncSettingsPageLimit)
	require.Len(t, transport.Requests, 2, "a full page without a total count is followed by a request for the next page")
	assert.Equal(t, []string{"100"}, transport.Requests[1].Query["offset"])
}
