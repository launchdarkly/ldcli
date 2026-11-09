package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// The endpoint rejects a request that names a kind it has no setting for,
// and the rejection fails every resource of the request, so each kind is
// asked for alone.
func TestSyncDirectionsAsksForEachKindAlone(t *testing.T) {
	transport := &recordingClient{Responses: [][]byte{
		[]byte(`{"items": [{"resourceKind":"tool","resourceLookupKey":"search","direction":"server_canonical"}]}`),
		[]byte(`{"items": [{"resourceKind":"variation","resourceLookupKey":"support/default","direction":"code_canonical"}]}`),
	}}
	client := NewClient(transport, "token", "https://example.com")
	variation := syncdomain.VariationID("project", "support", "default")
	tool := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}

	directions, err := client.SyncDirections("project", []syncdomain.ResourceID{variation, tool})

	require.NoError(t, err)
	assert.Equal(t, syncdomain.SyncDirectionCodeCanonical, directions[variation])
	assert.Equal(t, syncdomain.SyncDirectionServerCanonical, directions[tool])
	require.Len(t, transport.Requests, 2, "one request per kind, in kind order")
	for _, request := range transport.Requests {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "https://example.com/api/v2/projects/project/configs/sync/settings", request.Path)
	}
	assert.Equal(t, []string{"tool:search"}, transport.Requests[0].Query["resources"])
	assert.Equal(t, []string{"variation:support/default"}, transport.Requests[1].Query["resources"])
}

// LaunchDarkly answers 400 "unsupported resource kind" for a kind that has no
// sync setting yet. The variations of the same call still get their settings,
// and the resources of the rejected kind have none.
func TestSyncDirectionsTreatsAnUnsupportedKindAsHavingNoSetting(t *testing.T) {
	unsupported := errors.New(`{"code":"invalid_request","message":"unsupported resource kind","statusCode":400}`)
	// The kinds are asked for in name order: skill, tool, variation.
	transport := &recordingClient{
		Errors: []error{unsupported, unsupported, nil},
		Responses: [][]byte{nil, nil,
			[]byte(`{"items": [{"resourceKind":"variation","resourceLookupKey":"support/default","direction":"both"}]}`),
		},
	}
	client := NewClient(transport, "token", "https://example.com")
	variation := syncdomain.VariationID("project", "support", "default")
	tool := syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "project", LookupKey: "search"}
	skill := syncdomain.ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "project", LookupKey: "refunds"}

	directions, err := client.SyncDirections("project", []syncdomain.ResourceID{variation, tool, skill})

	require.NoError(t, err, "a kind without settings does not fail the variations")
	assert.Equal(t, map[syncdomain.ResourceID]syncdomain.SyncDirection{variation: syncdomain.SyncDirectionBoth}, directions)
	require.Len(t, transport.Requests, 3)
	assert.Equal(t, []string{"variation:support/default"}, transport.Requests[2].Query["resources"])

	other := errors.New(`{"code":"invalid_request","message":"resource key is not valid","statusCode":400}`)
	transport = &recordingClient{Errors: []error{other}}
	client = NewClient(transport, "token", "https://example.com")
	_, err = client.SyncDirections("project", []syncdomain.ResourceID{tool})
	require.Error(t, err, "another 400 still fails the call")
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
