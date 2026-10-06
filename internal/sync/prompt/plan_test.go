package prompt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestBuildPlanThreeWayMatrix(t *testing.T) {
	baseline := testVariation("baseline")
	localChange := testVariation("local")
	serverChange := testVariation("server")

	tests := []struct {
		name     string
		local    *syncdomain.Variation
		server   *syncdomain.Variation
		expected Action
	}{
		{"unchanged", &baseline, &baseline, ActionInSync},
		{"local edit", &localChange, &baseline, ActionUpdateServer},
		{"server edit", &baseline, &serverChange, ActionUpdateLocal},
		{"same edit", &localChange, &localChange, ActionUpdateManifest},
		{"divergent edits", &localChange, &serverChange, ActionConflict},
		{"local deletion", nil, &baseline, ActionArchiveServer},
		{"server deletion", &baseline, nil, ActionDeleteLocal},
		{"both deleted", nil, nil, ActionRemoveManifest},
		{"local edit after server deletion", &localChange, nil, ActionConflict},
		{"server edit after local deletion", nil, &serverChange, ActionConflict},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := testResourceID()
			baselineFingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, baseline)
			require.NoError(t, err)
			manifest := syncmanifest.Manifest{
				FormatVersion: syncmanifest.FormatVersion,
				Resources: []syncmanifest.Resource{{
					ResourceKind: id.Kind,
					ProjectKey:   id.ProjectKey,
					LookupKey:    id.LookupKey,
					Fingerprint:  baselineFingerprint,
				}},
			}

			plan := BuildPlan(manifest, localResources(test.local, false), map[ResourceID]ServerResource{
				id: {Variation: test.server, ConfigMode: syncdomain.VariationModeAgent},
			})
			require.Len(t, plan.Resources, 1)
			require.Equal(t, test.expected, plan.Resources[0].Action)
		})
	}
}

func TestBuildPlanFirstSync(t *testing.T) {
	local := testVariation("local")
	different := testVariation("server")
	id := testResourceID()

	tests := []struct {
		name     string
		local    *syncdomain.Variation
		server   *syncdomain.Variation
		upsert   bool
		expected Action
	}{
		{"equal resources are adopted", &local, &local, false, ActionUpdateManifest},
		{"different resources conflict", &local, &different, true, ActionConflict},
		{"upsert creates missing server resource", &local, nil, true, ActionCreateServer},
		{"missing server without upsert is an error", &local, nil, false, ActionError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := BuildPlan(syncmanifest.New(), localResources(test.local, test.upsert), map[ResourceID]ServerResource{
				id: {Variation: test.server, ConfigMode: syncdomain.VariationModeAgent},
			})
			require.Equal(t, test.expected, plan.Resources[0].Action)
		})
	}
}

func TestBuildPlanRejectsParentConfigModeMismatch(t *testing.T) {
	local := testVariation("local")
	id := testResourceID()

	plan := BuildPlan(syncmanifest.New(), localResources(&local, true), map[ResourceID]ServerResource{
		id: {ConfigMode: syncdomain.VariationModeCompletion},
	})

	require.Equal(t, ActionError, plan.Resources[0].Action)
	require.Contains(t, plan.Resources[0].Error, "does not match config mode")
}

func testResourceID() ResourceID {
	return ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default"}
}

func testVariation(name string) syncdomain.Variation {
	return syncdomain.Variation{
		Mode:         syncdomain.VariationModeAgent,
		Key:          "default",
		Name:         name,
		Instructions: "Help",
	}
}

func localResources(variation *syncdomain.Variation, upsert bool) []syncdomain.SyncedResource {
	if variation == nil {
		return nil
	}
	payload, _ := json.Marshal(variation)
	id := testResourceID()
	return []syncdomain.SyncedResource{{
		Kind:       id.Kind,
		ProjectKey: id.ProjectKey,
		LookupKey:  id.LookupKey,
		Payload:    payload,
		Upsert:     upsert,
	}}
}
