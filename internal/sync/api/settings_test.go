package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lderrors "github.com/launchdarkly/ldcli/internal/errors"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestClientResolveSourceOfTruthSendsAtMostOneHundredResources(t *testing.T) {
	ids := make([]syncdomain.ResourceID, 0, 150)
	for index := range 150 {
		ids = append(ids, syncdomain.VariationID("project", "support", fmt.Sprintf("v%03d", index)))
	}
	response := func(from, to int) []byte {
		items := make([]map[string]string, 0, to-from)
		for _, id := range ids[from:to] {
			items = append(items, map[string]string{
				"resourceKind": "variation", "resourceLookupKey": id.LookupKey, "direction": "both",
			})
		}
		items[0]["direction"] = "code_canonical"
		data, err := json.Marshal(map[string]any{"items": items})
		require.NoError(t, err)
		return data
	}
	transport := &recordingServer{Responses: [][]byte{response(0, 100), response(100, 150)}}
	client := transport.client(t)

	sources, err := client.ResolveSourceOfTruth("project", ids)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 2)
	for _, request := range transport.Requests {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Equal(t, "/api/v2/projects/project/configs/sync/settings", request.Path)
		assert.Empty(t, request.Body)
	}
	assert.Len(t, transport.Requests[0].Query["resources"], 100)
	assert.Len(t, transport.Requests[1].Query["resources"], 50)
	assert.Equal(t, "variation:support/v000", transport.Requests[0].Query["resources"][0])
	assert.Equal(t, "variation:support/v100", transport.Requests[1].Query["resources"][0])
	require.Len(t, sources, 150)
	assert.Equal(t, syncdomain.SourceOfTruthCodeOnly, sources[ids[0]])
	assert.Equal(t, syncdomain.SourceOfTruthUIAndCode, sources[ids[1]])
	assert.Equal(t, syncdomain.SourceOfTruthCodeOnly, sources[ids[100]])
}

func TestClientResolveSourceOfTruthUsesUIAndCodeForMissingItems(t *testing.T) {
	id := syncdomain.VariationID("project", "support", "default")
	transport := &recordingServer{Responses: [][]byte{[]byte(`{"items":[]}`)}}
	client := transport.client(t)

	sources, err := client.ResolveSourceOfTruth("project", []syncdomain.ResourceID{id})

	require.NoError(t, err)
	assert.Equal(t, map[syncdomain.ResourceID]syncdomain.SourceOfTruth{id: syncdomain.SourceOfTruthUIAndCode}, sources)
}

func TestClientResolveSourceOfTruthTreatsServerCanonicalAsUIAndCode(t *testing.T) {
	id := syncdomain.VariationID("project", "support", "default")
	transport := &recordingServer{Responses: [][]byte{[]byte(
		`{"items":[{"resourceKind":"variation","resourceLookupKey":"support/default","direction":"server_canonical"}]}`,
	)}}

	sources, err := transport.client(t).ResolveSourceOfTruth("project", []syncdomain.ResourceID{id})

	require.NoError(t, err)
	assert.Equal(t, syncdomain.SourceOfTruthUIAndCode, sources[id])
}

func TestClientResolveSourceOfTruthSendsNoRequestWithoutResources(t *testing.T) {
	transport := &recordingServer{}

	sources, err := transport.client(t).ResolveSourceOfTruth("project", nil)

	require.NoError(t, err)
	assert.Empty(t, sources)
	assert.Empty(t, transport.Requests)
}

func TestClientResolveSourceOfTruthReturnsSentinelWhenNotFound(t *testing.T) {
	transport := &recordingServer{Err: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`)}

	_, err := transport.client(t).ResolveSourceOfTruth("project", []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "default")})

	require.ErrorIs(t, err, ErrSyncSettingsUnavailable)
}

func TestClientResolveSourceOfTruthReturnsOtherErrors(t *testing.T) {
	for _, body := range []string{
		`{"code":"forbidden","message":"Access denied","statusCode":403}`,
		`{"code":"internal","message":"boom","statusCode":500}`,
	} {
		transport := &recordingServer{Err: lderrors.NewError(body)}

		_, err := transport.client(t).ResolveSourceOfTruth("project", []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "default")})

		require.Error(t, err, body)
		assert.NotErrorIs(t, err, ErrSyncSettingsUnavailable, body)
		assert.ErrorContains(t, err, "get sync settings for project", body)
	}
}

func TestClientResolveSourceOfTruthRejectsUnknownDirection(t *testing.T) {
	transport := &recordingServer{Responses: [][]byte{[]byte(
		`{"items":[{"resourceKind":"variation","resourceLookupKey":"support/default","direction":"sideways"}]}`,
	)}}
	client := transport.client(t)

	_, err := client.ResolveSourceOfTruth("project", []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "default")})

	require.ErrorContains(t, err, `unsupported sync direction "sideways"`)
}

func TestClientPatchSyncSettingsUpsertsExactRule(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	ids := []syncdomain.ResourceID{syncdomain.VariationID("project", "support-bot", "default")}

	err := client.PatchSyncSettings("project", ExactVariationRules(ids, syncdomain.SourceOfTruthCodeOnly), nil)

	require.NoError(t, err)
	require.Len(t, transport.Requests, 1)
	request := transport.Requests[0]
	assert.Equal(t, http.MethodPatch, request.Method)
	assert.Equal(t, "/api/v2/projects/project/configs/sync/settings", request.Path)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support-bot/default","direction":"code_canonical"}],
		"deletions": []
	}`, string(request.Body))
}

func TestClientPatchSyncSettingsDeletesExactRule(t *testing.T) {
	transport := &recordingServer{}
	client := transport.client(t)
	ids := []syncdomain.ResourceID{syncdomain.VariationID("project", "support-bot", "default")}

	require.NoError(t, client.PatchSyncSettings("project", nil, ExactVariationPatterns(ids)))

	require.Len(t, transport.Requests, 1)
	assert.JSONEq(t, `{
		"upserts": [],
		"deletions": [{"resourceKind":"variation","resourcePattern":"support-bot/default"}]
	}`, string(transport.Requests[0].Body))
}

func TestClientPatchSyncSettingsSendsAtMostOneHundredRulesInEachList(t *testing.T) {
	ids := make([]syncdomain.ResourceID, 0, 150)
	for index := range 150 {
		ids = append(ids, syncdomain.VariationID("project", "support", fmt.Sprintf("v%03d", index)))
	}
	transport := &recordingServer{}
	client := transport.client(t)

	require.NoError(t, client.PatchSyncSettings("project", ExactVariationRules(ids, syncdomain.SourceOfTruthCodeOnly), nil))

	require.Len(t, transport.Requests, 2)
	var first, second struct {
		Upserts   []SyncSettingRule    `json:"upserts"`
		Deletions []SyncSettingPattern `json:"deletions"`
	}
	require.NoError(t, json.Unmarshal(transport.Requests[0].Body, &first))
	require.NoError(t, json.Unmarshal(transport.Requests[1].Body, &second))
	assert.Len(t, first.Upserts, 100)
	assert.Len(t, second.Upserts, 50)
	assert.Equal(t, "support/v100", second.Upserts[0].ResourcePattern)
	assert.NotNil(t, second.Deletions)
}

func TestClientPatchSyncSettingsReturnsSentinelWhenNotFound(t *testing.T) {
	transport := &recordingServer{Err: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`)}
	ids := []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "default")}

	err := transport.client(t).PatchSyncSettings("project", ExactVariationRules(ids, syncdomain.SourceOfTruthCodeOnly), nil)

	require.ErrorIs(t, err, ErrSyncSettingsUnavailable)
}

func TestClientPatchSyncSettingsReturnsOtherErrors(t *testing.T) {
	for _, body := range []string{
		`{"code":"forbidden","message":"Access denied","statusCode":403}`,
		`{"code":"internal","message":"boom","statusCode":500}`,
	} {
		transport := &recordingServer{Err: lderrors.NewError(body)}
		ids := []syncdomain.ResourceID{syncdomain.VariationID("project", "support", "default")}

		err := transport.client(t).PatchSyncSettings("project", nil, ExactVariationPatterns(ids))

		require.Error(t, err, body)
		assert.NotErrorIs(t, err, ErrSyncSettingsUnavailable, body)
	}
}
