package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		{"missing local file is restored", nil, &baseline, ActionUpdateLocal},
		{"server deletion", &baseline, nil, ActionDeleteLocal},
		{"both deleted", nil, nil, ActionRemoveManifest},
		{"local edit after server deletion", &localChange, nil, ActionConflict},
		{"missing local file after a server edit is restored", nil, &serverChange, ActionUpdateLocal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := testResourceID()
			baselineFingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, baseline)
			require.NoError(t, err)
			manifest := syncmanifest.Manifest{
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

func TestBuildPlanSourceOfTruthMatrix(t *testing.T) {
	baseline := testVariation("baseline")
	localChange, serverChange := baseline, baseline
	localChange.Instructions, serverChange.Instructions = "Local prompt", "Server prompt"
	localSettings, serverSettings := baseline, baseline
	localSettings.Name, serverSettings.Name = "local", "server"
	localPromptAndSettings := localChange
	localPromptAndSettings.Name = "local"
	serverPromptAndSettings := serverChange
	serverPromptAndSettings.Name = "server"
	both, code := false, true

	type expectation struct {
		action  Action
		reverts bool
		err     string
	}
	tests := []struct {
		name     string
		tracked  bool
		local    *syncdomain.Variation
		server   *syncdomain.Variation
		upsert   bool
		codeOnly bool
		expected expectation
	}{
		{"only local changed, both", true, &localChange, &baseline, false, both, expectation{action: ActionUpdateServer}},
		{"only local changed, code", true, &localChange, &baseline, false, code, expectation{action: ActionUpdateServer}},

		{"only LaunchDarkly changed, both", true, &baseline, &serverChange, false, both, expectation{action: ActionUpdateLocal}},
		{"only LaunchDarkly changed, code", true, &baseline, &serverChange, false, code,
			expectation{action: ActionUpdateServer, reverts: true}},

		{"both changed, both", true, &localChange, &serverChange, false, both, expectation{action: ActionConflict}},
		{"both changed, code", true, &localChange, &serverChange, false, code,
			expectation{action: ActionUpdateServer, reverts: true}},

		{"new local file with upsert, both", false, &localChange, nil, true, both, expectation{action: ActionCreateServer}},
		{"new local file with upsert, code", false, &localChange, nil, true, code, expectation{action: ActionCreateServer}},
		{"new local file without upsert, both", false, &localChange, nil, false, both,
			expectation{action: ActionError, err: "set upsert: true"}},
		{"new local file without upsert, code", false, &localChange, nil, false, code,
			expectation{action: ActionError, err: "set upsert: true"}},

		{"server deletion, both", true, &baseline, nil, false, both, expectation{action: ActionDeleteLocal}},
		{"server deletion, code", true, &baseline, nil, false, code, expectation{action: ActionCreateServer, reverts: true}},

		{"local edit after server deletion, code", true, &localChange, nil, false, code,
			expectation{action: ActionCreateServer, reverts: true}},

		{"missing local file, both", true, nil, &serverChange, false, both, expectation{action: ActionUpdateLocal}},
		{"missing local file, code", true, nil, &serverChange, false, code, expectation{action: ActionUpdateLocal}},

		{"no baseline and different, both", false, &localChange, &serverChange, false, both, expectation{action: ActionConflict}},
		{"no baseline and different, code", false, &localChange, &serverChange, false, code,
			expectation{action: ActionUpdateServer, reverts: true}},

		{"only LaunchDarkly changed other settings, code", true, &baseline, &serverSettings, false, code,
			expectation{action: ActionUpdateLocal}},
		{"LaunchDarkly changed the prompt and other settings, code", true, &baseline, &serverPromptAndSettings, false, code,
			expectation{action: ActionUpdateBoth, reverts: true}},
		{"local changed the prompt, LaunchDarkly changed other settings, code", true, &localChange, &serverSettings, false, code,
			expectation{action: ActionUpdateBoth}},
		{"local changed other settings, LaunchDarkly changed the prompt, code", true, &localSettings, &serverChange, false, code,
			expectation{action: ActionUpdateServer, reverts: true}},
		{"both changed the prompt and other settings differently, code", true, &localPromptAndSettings, &serverPromptAndSettings, false, code,
			expectation{action: ActionConflict}},
		{"both changed other settings differently, code", true, &localSettings, &serverSettings, false, code,
			expectation{action: ActionConflict}},
		{"no baseline and different other settings, code", false, &localSettings, &serverSettings, false, code,
			expectation{action: ActionConflict}},

		{"unchanged, code", true, &baseline, &baseline, false, code, expectation{action: ActionInSync}},
		{"same edit, code", true, &localChange, &localChange, false, code, expectation{action: ActionUpdateManifest}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id := testResourceID()
			manifest := syncmanifest.New()
			if test.tracked {
				baselineFingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, baseline)
				require.NoError(t, err)
				manifest.SetFingerprint(id, baselineFingerprint)
			}

			plan := BuildPlan(manifest, localResources(test.local, test.upsert), map[ResourceID]ServerResource{
				id: {Variation: test.server, ConfigMode: syncdomain.VariationModeAgent, CodeOnly: test.codeOnly},
			})

			require.Len(t, plan.Resources, 1)
			resource := plan.Resources[0]
			assert.Equal(t, test.expected.action, resource.Action)
			assert.Equal(t, test.codeOnly, resource.CodeOnly)
			assert.Equal(t, test.expected.reverts, resource.RevertsServer, "reverts")
			if test.expected.err == "" {
				assert.Empty(t, resource.Error)
			} else {
				assert.Contains(t, resource.Error, test.expected.err)
			}
		})
	}
}

func TestBuildPlanUpdatesStalePinsOfCodeOnlyRevert(t *testing.T) {
	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	baseline := testVariation("baseline")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	baseline.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Tool: &tool}}
	server := testVariation("baseline")
	server.Instructions = "Changed in LaunchDarkly"
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	server.Attachments = []syncdomain.Attachment{{Kind: syncdomain.AttachmentTool, Version: 2, Tool: &tool}}

	id := testResourceID()
	fingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, baseline)
	require.NoError(t, err)
	manifest := syncmanifest.New()
	manifest.SetFingerprint(id, fingerprint)

	plan := BuildPlan(manifest, localResources(&baseline, false), map[ResourceID]ServerResource{
		id: {Variation: &server, ConfigMode: syncdomain.VariationModeAgent, CodeOnly: true},
	})

	resource := plan.Resources[0]
	assert.Equal(t, ActionUpdateServer, resource.Action)
	assert.True(t, resource.RevertsServer)
	assert.True(t, resource.ServerHasStaleAttachmentPins)
	assert.Contains(t, resource.Diff, "attachment versions")
}

func TestPlanHasOverwrites(t *testing.T) {
	assert.False(t, Plan{Resources: []PlannedResource{{Action: ActionUpdateServer}}}.HasOverwrites())
	assert.True(t, Plan{Resources: []PlannedResource{{Action: ActionUpdateServer, RevertsServer: true}}}.HasOverwrites())
}

func TestBuildPlanDoesNotTreatManifestAttachmentsAsIndependentTargets(t *testing.T) {
	manifest := syncmanifest.New()
	manifest.Resources = []syncmanifest.Resource{{
		ResourceKind: syncdomain.KindTool,
		ProjectKey:   "project",
		LookupKey:    "search",
		Fingerprint:  "sha256:ignored",
	}}

	plan := BuildPlan(manifest, nil, nil)

	require.Empty(t, plan.Resources)
}

func TestBuildPlanOmitsDiffForEquivalentAPIDefaults(t *testing.T) {
	local := testVariation("example")
	local.Model = map[string]any{"modelName": "gpt-5"}
	server := local
	server.Model = map[string]any{
		"modelName":  "gpt-5",
		"custom":     map[string]any{},
		"parameters": map[string]any{},
	}

	id := testResourceID()
	fingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, server)
	require.NoError(t, err)
	manifest := syncmanifest.New()
	manifest.SetFingerprint(id, fingerprint)

	plan := BuildPlan(manifest, localResources(&local, false), map[ResourceID]ServerResource{
		id: {Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
	})

	require.Equal(t, ActionInSync, plan.Resources[0].Action)
	require.Empty(t, plan.Resources[0].Diff)
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

func TestPlanHasDestructiveActions(t *testing.T) {
	tests := map[string]struct {
		action      Action
		destructive bool
	}{
		"create server": {action: ActionCreateServer},
		"update server": {action: ActionUpdateServer},
		"update local":  {action: ActionUpdateLocal},
		"delete local":  {action: ActionDeleteLocal, destructive: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			plan := Plan{Resources: []PlannedResource{{Action: test.action}}}
			require.Equal(t, test.destructive, plan.HasDestructiveActions())
		})
	}
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
	id := testResourceID()
	return []syncdomain.SyncedResource{{
		Kind:       id.Kind,
		ProjectKey: id.ProjectKey,
		LookupKey:  id.LookupKey,
		Variation:  *variation,
		Upsert:     upsert,
	}}
}
