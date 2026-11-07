package prompt

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclink "github.com/launchdarkly/ldcli/internal/sync/link"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
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

func TestIndexedSelectionBaselineBlocksReorderedItems(t *testing.T) {
	reference := syncdomain.Reference{
		File: "agents.json", Format: "json", Select: "agents[0]",
		Map: map[string]string{"prompt": "prompt"},
	}
	digest, err := syncdomain.ReferenceDigest(reference)
	require.NoError(t, err)
	baselineSelection := syncdomain.IndexedSelection{
		Path: "select#0", CollectionLength: 2, Fingerprint: "sha256:" + strings.Repeat("a", 64),
	}
	baseline := syncmanifest.Resource{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/default",
		Fingerprint: "sha256:" + strings.Repeat("b", 64), FingerprintSchema: syncdomain.ProjectionFingerprintSchema,
		ReferenceDigest: digest, AdapterBehavior: "json/1",
		IndexedSelections: []syncdomain.IndexedSelection{baselineSelection},
	}
	resource := PlannedResource{
		ID:     syncdomain.VariationID("project", "config", "default"),
		Action: ActionUpdateServer, Source: &reference,
		IndexedSelections: []syncdomain.IndexedSelection{baselineSelection},
	}

	checkSourceBaseline(&resource, baseline, true)
	assert.Equal(t, ActionUpdateServer, resource.Action)

	resource.IndexedSelections[0].Fingerprint = "sha256:" + strings.Repeat("c", 64)
	checkSourceBaseline(&resource, baseline, true)
	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorIndexedSelectionChanged, resource.Problem.Code)
	assert.Equal(t, ActionError, resource.Action)

	// A lock written before indexed selections were recorded has none, like
	// the other optional baseline fields, and that is not a change.
	older := baseline
	older.IndexedSelections = nil
	resource = PlannedResource{
		ID:     syncdomain.VariationID("project", "config", "default"),
		Action: ActionUpdateServer, Source: &reference,
		IndexedSelections: []syncdomain.IndexedSelection{baselineSelection},
	}
	checkSourceBaseline(&resource, older, true)
	assert.Nil(t, resource.Problem)
	assert.Equal(t, ActionUpdateServer, resource.Action)
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

func TestDescriptorRelationshipsUseServerConfigMode(t *testing.T) {
	id := testResourceID()
	tools := []syncdomain.AttachmentRef{{Key: "search"}}
	skills := []syncdomain.AttachmentRef{{Key: "support"}}
	reference := syncdomain.Reference{
		File: "prompt.md", Format: "markdown", Tools: &tools, Skills: &skills,
	}
	local := syncdomain.SyncedResource{
		Kind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey,
		Source: &reference, Upsert: true,
		Variation: syncdomain.Variation{
			Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
			Tools: tools, Skills: skills,
		},
		Projection: syncdomain.VariationProjection{
			Mode:        syncdomain.VariationModeAgent,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{},
		},
	}

	row := planResource(id, &local, ServerResource{
		ConfigMode: syncdomain.VariationModeCompletion,
	}, "", false)

	assert.Equal(t, ActionError, row.Action)
	assert.Equal(t, "skills can only be attached to agent-mode configs", row.Error)

	local.Variation.Skills = nil
	reference.Skills = nil
	local.Source = &reference
	row = planResource(id, &local, ServerResource{
		ConfigMode: syncdomain.VariationModeCompletion,
	}, "", false)
	assert.NotEqual(t, ActionError, row.Action)
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

func TestBuildPlanKeepsRealModelParameterChangesWithExpandedTools(t *testing.T) {
	local := testVariation("example")
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Model = map[string]any{
		"modelName": "gpt-5",
		"parameters": map[string]any{
			"temperature": 0.2,
		},
	}
	server := local
	server.Model = map[string]any{
		"modelName": "gpt-5",
		"parameters": map[string]any{
			"temperature": 0.7,
			"tools":       []any{map[string]any{"type": "function", "name": "search"}},
		},
	}

	id := testResourceID()
	manifest := syncmanifest.New()
	manifest.SetFingerprint(id, fingerprintOf(t, local))
	plan := BuildPlan(manifest, localResources(&local, false), map[ResourceID]ServerResource{
		id: {Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
	})

	require.Equal(t, ActionUpdateLocal, plan.Resources[0].Action)
	require.Equal(t, "model changed in LaunchDarkly", planReason(plan.Resources[0]))
	require.NotContains(t, string(plan.Resources[0].Diff["variation"].Before), `"tools"`)
	require.Contains(t, string(plan.Resources[0].Diff["variation"].Before), `"temperature":0.7`)
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

func TestPlanBlockingErrorHasTheFullErrorOfEveryRow(t *testing.T) {
	conflicted, missing, broken := testResourceID(), testResourceID(), testResourceID()
	missing.LookupKey, broken.LookupKey = "support/missing", "support/broken"
	plan := Plan{Resources: []PlannedResource{
		{ID: conflicted, Action: ActionConflict},
		{ID: missing, Action: ActionError, Error: "variation does not exist in LaunchDarkly; set upsert: true to create it"},
		{ID: testResourceID(), Action: ActionInSync},
		{ID: broken, Action: ActionError, Error: "invalid server variation: bad mode"},
	}}

	require.EqualError(t, plan.BlockingError(), "cannot sync conflicted resource variation production/support/default\n"+
		"cannot sync variation production/support/missing: variation does not exist in LaunchDarkly; set upsert: true to create it\n"+
		"cannot sync variation production/support/broken: invalid server variation: bad mode")
	require.NoError(t, Plan{Resources: []PlannedResource{{ID: testResourceID(), Action: ActionUpdateServer}}}.BlockingError())
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

func TestPlanAttachmentThreeWayRules(t *testing.T) {
	for _, kind := range []syncdomain.AttachmentKind{syncdomain.AttachmentTool, syncdomain.AttachmentSkill} {
		baseline, localChange, serverChange := testAttachment(kind, "baseline"), testAttachment(kind, "local"), testAttachment(kind, "server")
		withUpsert := localChange
		withUpsert.Upsert = true
		id := baseline.ID("production")

		tests := []struct {
			name     string
			tracked  bool
			local    *syncdomain.Attachment
			server   *syncdomain.Attachment
			expected Action
		}{
			{"untracked equal content is adopted", false, &baseline, &baseline, ActionUpdateManifest},
			{"untracked different content conflicts", false, &localChange, &serverChange, ActionConflict},
			{"untracked upsert creates", false, &withUpsert, nil, ActionCreateServer},
			{"untracked without upsert is an error", false, &localChange, nil, ActionError},
			{"unchanged", true, &baseline, &baseline, ActionInSync},
			{"local edit", true, &localChange, &baseline, ActionUpdateServer},
			{"server edit", true, &baseline, &serverChange, ActionUpdateLocal},
			{"same edit", true, &localChange, &localChange, ActionUpdateManifest},
			{"divergent edits", true, &localChange, &serverChange, ActionConflict},
			{"missing local file is restored", true, nil, &baseline, ActionUpdateLocal},
			{"server deletion", true, &baseline, nil, ActionDeleteLocal},
			{"local edit after server deletion", true, &localChange, nil, ActionConflict},
			{"both deleted", true, nil, nil, ActionRemoveManifest},
		}

		for _, test := range tests {
			t.Run(string(kind)+" "+test.name, func(t *testing.T) {
				baselineFingerprint := ""
				if test.tracked {
					baselineFingerprint = attachmentFingerprint(t, baseline)
				}

				resource := planAttachment(id, test.local, test.server, baselineFingerprint, test.tracked)

				require.Equal(t, test.expected, resource.Action)
				if resource.LocalFingerprint != resource.ServerFingerprint {
					require.Equal(t, []ResourceID{id}, resource.changedAttachments)
					require.Contains(t, resource.Diff, string(kind)+"s")
				}
			})
		}
	}
}

// LaunchDarkly owns the name of a skill, so a rename there is not a change.
func TestPlanAttachmentIgnoresTheSkillName(t *testing.T) {
	local, server := testAttachment(syncdomain.AttachmentSkill, "same"), testAttachment(syncdomain.AttachmentSkill, "same")
	local.Skill.Name, server.Skill.Name = "Summarize", "Summarize a ticket"

	resource := planAttachment(local.ID("production"), &local, &server, attachmentFingerprint(t, local), true)

	assert.Equal(t, ActionInSync, resource.Action)
	assert.Empty(t, resource.Diff)
}

func TestPlanAttachmentExplainsHowToCreateOrStopSyncing(t *testing.T) {
	tool, skill := testAttachment(syncdomain.AttachmentTool, "local"), testAttachment(syncdomain.AttachmentSkill, "local")

	assert.Equal(t, `tool "search" does not exist in LaunchDarkly. `+
		`To create it, add "upsert": true to its file and run ldcli sync tools --project production --key search. `+
		"To stop syncing it, run ldcli sync tools detach --project production --key search.",
		planAttachment(tool.ID("production"), &tool, nil, "", false).Error)
	assert.Equal(t, `skill "summarize" does not exist in LaunchDarkly. `+
		"To create it, add upsert: true to its front matter and run ldcli sync skills --project production --key summarize. "+
		"To stop syncing it, run ldcli sync skills detach --project production --key summarize.",
		planAttachment(skill.ID("production"), &skill, nil, "", false).Error)
}

func TestPlanRejectsACreateThatLaunchDarklyWouldReject(t *testing.T) {
	skill := testAttachment(syncdomain.AttachmentSkill, "local")
	skill.Skill.Key, skill.Upsert = "Summarize_Ticket", true
	resource := planAttachment(skill.ID("production"), &skill, nil, "", false)
	assert.Equal(t, ActionError, resource.Action)
	assert.Equal(t, `skill key "Summarize_Ticket" must use only lowercase letters, digits, and hyphens, and start with a letter or digit`, resource.Error)

	variation := testVariation("local")
	variation.Key = "default variation"
	plan := BuildPlan(syncmanifest.New(), []syncdomain.SyncedResource{{
		Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default variation", Upsert: true, Variation: variation,
	}}, map[ResourceID]ServerResource{
		syncdomain.VariationID("production", "support", "default variation"): {ConfigMode: syncdomain.VariationModeAgent},
	})
	assert.Equal(t, ActionError, plan.Resources[0].Action)
	assert.Contains(t, plan.Resources[0].Error, `variation key "default variation" must use only letters`)
}

func TestValidateCreateMirrorsLaunchDarklyRules(t *testing.T) {
	long := strings.Repeat("a", 65)
	tests := []struct {
		kind              syncdomain.Kind
		key, description  string
		wantErrorContains string
	}{
		{kind: syncdomain.KindSkill, key: "incident-summary"},
		{kind: syncdomain.KindSkill, key: "-summary", wantErrorContains: "start with a letter or digit"},
		{kind: syncdomain.KindSkill, key: long, wantErrorContains: "at most 64 characters"},
		{kind: syncdomain.KindSkill, key: "com1", wantErrorContains: "reserved file name on Windows"},
		{kind: syncdomain.KindTool, key: "lookup_order.v2"},
		{kind: syncdomain.KindTool, key: "lookup order", wantErrorContains: "must use only letters"},
		{kind: syncdomain.KindTool, key: strings.Repeat("a", 257), wantErrorContains: "at most 256 characters"},
		{kind: syncdomain.KindTool, key: "lookup", description: strings.Repeat("d", 1025), wantErrorContains: "description must be at most 1024"},
	}

	for _, test := range tests {
		err := syncdomain.ValidateCreate(test.kind, test.key, "", test.description)
		if test.wantErrorContains == "" {
			assert.NoError(t, err, test.key)
		} else {
			assert.ErrorContains(t, err, test.wantErrorContains, test.key)
		}
	}
}

// Sync applies a plan in identity order. Skills and tools must come before the
// variations that pin them.
func TestPlanOrderAppliesSkillsAndToolsBeforeVariations(t *testing.T) {
	variation := testResourceID()
	tool := ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"}
	skill := ResourceID{Kind: syncdomain.KindSkill, ProjectKey: "production", LookupKey: "summarize"}
	ids := []ResourceID{variation, tool, skill}

	slices.SortFunc(ids, syncdomain.CompareResourceIDs)

	assert.Equal(t, []ResourceID{skill, tool, variation}, ids)
}

// A sync must not publish a tool edit through one variation and write the old
// content back to the same file through another variation.
func TestCheckAttachmentBaselinesStopsAVariationFromOverwritingAnUnsyncedEdit(t *testing.T) {
	old, edited := testAttachment(syncdomain.AttachmentTool, "old"), testAttachment(syncdomain.AttachmentTool, "edited")
	withTool := func(name string, tool syncdomain.Attachment) *syncdomain.Variation {
		variation := testVariation(name)
		variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
		variation.Attachments = []syncdomain.Attachment{tool}
		return &variation
	}
	pushEdit := PlannedResource{ID: testResourceID(), Action: ActionUpdateServer, Local: withTool("first", edited), Server: withTool("first", old)}
	pullOld := PlannedResource{ID: testResourceID(), Action: ActionUpdateLocal, Local: withTool("second", edited), Server: withTool("second", old)}
	pullOld.ID.LookupKey = "support/second"
	withoutTool := testVariation("third")
	createTool := PlannedResource{ID: testResourceID(), Action: ActionUpdateServer, Local: withTool("third", edited), Server: &withoutTool}
	createTool.ID.LookupKey = "support/third"
	resources := []PlannedResource{pushEdit, pullOld, createTool}
	for index := range resources {
		resources[index].changedAttachments = changedAttachmentIDs(resources[index])
	}
	lock := syncmanifest.New()
	lock.SetFingerprint(old.ID("production"), attachmentFingerprint(t, old))

	require.NoError(t, checkAttachmentBaselines(t.TempDir(), resources, lock))

	assert.Equal(t, ActionUpdateServer, resources[0].Action, "LaunchDarkly still has the recorded tool")
	assert.Equal(t, ActionConflict, resources[1].Action, "the pull would overwrite the local edit")
	assert.Equal(t, ActionUpdateServer, resources[2].Action, "LaunchDarkly has no copy of the tool on this variation")
	groups := groupConflicts(Plan{Resources: resources})
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].resources, 3, "one choice covers every variation that writes the tool")

	untracked := []PlannedResource{pushEdit}
	untracked[0].changedAttachments = changedAttachmentIDs(pushEdit)
	require.NoError(t, checkAttachmentBaselines(t.TempDir(), untracked, syncmanifest.New()))
	assert.Equal(t, ActionConflict, untracked[0].Action, "without a baseline, two versions of the tool conflict")
}

func TestCheckAttachmentDeletesKeepsAToolThatAVariationStillUses(t *testing.T) {
	tool := testAttachment(syncdomain.AttachmentTool, "local")
	user := testVariation("user")
	user.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	user.Attachments = []syncdomain.Attachment{tool}
	variations := []syncdomain.SyncedResource{{
		Kind: syncdomain.KindVariation, ProjectKey: "production", LookupKey: "support/default", Variation: user,
	}}
	deletion := PlannedResource{ID: tool.ID("production"), Action: ActionDeleteLocal}
	withoutTool := testVariation("user")

	tests := map[string]struct {
		variationRow *PlannedResource
		wantAction   Action
		wantError    string
	}{
		"the variation is not in the plan": {
			wantAction: ActionError,
			wantError: "tool production/search was deleted in LaunchDarkly, but variation production/support/default still uses it. " +
				"Run ldcli sync all to pull the variation first.",
		},
		"the variation pulls a version without the tool": {
			variationRow: &PlannedResource{ID: testResourceID(), Action: ActionUpdateLocal, Server: &withoutTool},
			wantAction:   ActionDeleteLocal,
		},
		"the variation pushes": {
			variationRow: &PlannedResource{ID: testResourceID(), Action: ActionUpdateServer, Local: &user},
			wantAction:   ActionError,
			wantError: "tool production/search was deleted in LaunchDarkly, but variation production/support/default still uses it. " +
				"Remove it from the variation first.",
		},
		"the variation is deleted": {
			variationRow: &PlannedResource{ID: testResourceID(), Action: ActionDeleteLocal, Local: &user},
			wantAction:   ActionDeleteLocal,
		},
		"the variation conflicts, and LaunchDarkly has no tool": {
			variationRow: &PlannedResource{ID: testResourceID(), Action: ActionConflict, Local: &user, Server: &withoutTool},
			wantAction:   ActionDeleteLocal,
		},
		"the variation conflicts, and LaunchDarkly keeps the tool": {
			variationRow: &PlannedResource{ID: testResourceID(), Action: ActionConflict, Local: &user, Server: &user},
			wantAction:   ActionError,
			wantError: "tool production/search was deleted in LaunchDarkly, but variation production/support/default still uses it. " +
				"Remove it from the variation first.",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resources := []PlannedResource{deletion}
			if test.variationRow != nil {
				resources = append(resources, *test.variationRow)
			}

			checkAttachmentDeletes(resources, variations)

			assert.Equal(t, test.wantAction, resources[0].Action)
			assert.Equal(t, test.wantError, resources[0].Error)
		})
	}
}

// A deleted tool stays out of the conflict group of a variation that uses it,
// so that a local choice cannot create the tool again. The check after the
// choice decides the delete.
func TestApplyConflictResolutionsChecksToolDeletesAgain(t *testing.T) {
	tool := testAttachment(syncdomain.AttachmentTool, "local")
	user := testVariation("user")
	user.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	user.Attachments = []syncdomain.Attachment{tool}
	withoutTool := testVariation("user")
	deletion := planAttachment(tool.ID("production"), &tool, nil, attachmentFingerprint(t, tool), true)
	require.Equal(t, ActionDeleteLocal, deletion.Action)
	conflict := PlannedResource{ID: testResourceID(), Action: ActionConflict, Local: &user, Server: &withoutTool}
	conflict.changedAttachments = changedAttachmentIDs(conflict)
	plan := Plan{Resources: []PlannedResource{deletion, conflict}}

	groups := groupConflicts(plan)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].resources, 1, "the deleted tool does not take the choice")
	choose := func(resolution ConflictResolution) Plan {
		resolutions := make(map[ResourceID]ConflictResolution)
		for _, member := range groups[0].resources {
			resolutions[member.ID] = resolution
		}
		return applyConflictResolutions(plan, resolutions)
	}

	pulled := choose(ConflictUseLaunchDarkly)
	assert.Equal(t, ActionDeleteLocal, pulled.Resources[0].Action)
	assert.Equal(t, ActionUpdateLocal, pulled.Resources[1].Action)
	require.NoError(t, pulled.BlockingError())

	pushed := choose(ConflictUseLocal)
	assert.Equal(t, ActionError, pushed.Resources[0].Action, "the local choice must not create the deleted tool")
	assert.Equal(t, ActionUpdateServer, pushed.Resources[1].Action)
	require.EqualError(t, pushed.BlockingError(), "cannot sync tool production/search: tool production/search was deleted in LaunchDarkly, "+
		"but variation production/support/default still uses it. Remove it from the variation first.")
	assert.Equal(t, ActionDeleteLocal, plan.Resources[0].Action, "the reviewed plan does not change")
}

func testAttachment(kind syncdomain.AttachmentKind, description string) syncdomain.Attachment {
	if kind == syncdomain.AttachmentSkill {
		return syncdomain.Attachment{Kind: kind, Skill: &syncdomain.Skill{Key: "summarize", Description: description, Markdown: "# Summarize\n"}}
	}
	return syncdomain.Attachment{Kind: kind, Tool: &syncdomain.Tool{Key: "search", Description: &description, Schema: map[string]any{"type": "object"}}}
}

func attachmentFingerprint(t *testing.T, attachment syncdomain.Attachment) string {
	t.Helper()
	fingerprint, err := syncdomain.FingerprintAttachment("production", attachment)
	require.NoError(t, err)
	return fingerprint
}

// sync.lock records the projection fingerprint of a linked tool, so the
// baseline check must project the overwritten copy the same way. Otherwise an
// unowned LaunchDarkly field makes every push of a variation a conflict.
func TestCheckAttachmentBaselinesProjectsALinkedTool(t *testing.T) {
	root := t.TempDir()
	writeTestDescriptor := func(relativePath, content string) {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	writeTestDescriptor("tool.json", `{"description":"Linked","schema":{"type":"object"}}`)
	writeTestDescriptor(".launchdarkly/production/tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n")
	local, err := synclocal.CompileAttachments(root, func(ResourceID) bool { return true })
	require.NoError(t, err)
	require.Len(t, local, 1)
	baseline, err := local[0].Fingerprint()
	require.NoError(t, err)
	lock := syncmanifest.New()
	lock.SetFingerprint(local[0].ID(), baseline)

	withTool := func(name string, tool syncdomain.Attachment) *syncdomain.Variation {
		variation := testVariation(name)
		variation.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
		variation.Attachments = []syncdomain.Attachment{tool}
		return &variation
	}

	// A field that only LaunchDarkly holds is not a change of the linked tool.
	unowned := local[0].Attachment
	unownedTool := *unowned.Tool
	unownedTool.CustomParameters = map[string]any{"timeout": 5.0}
	unowned.Tool = &unownedTool
	same := PlannedResource{
		ID: testResourceID(), Action: ActionUpdateServer,
		Local: withTool("local", local[0].Attachment), Server: withTool("server", unowned),
	}
	assert.Empty(t, changedAttachmentIDs(same))

	// The mapped description differs, and LaunchDarkly still has the
	// recorded one, so the push is not a conflict.
	serverCopy := local[0].Attachment
	serverTool := *serverCopy.Tool
	serverTool.Description = pointerTo("Recorded")
	serverCopy.Tool = &serverTool
	serverCopy.Projection = nil
	recorded, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: serverCopy, Source: local[0].Source}.Fingerprint()
	require.NoError(t, err)
	lock.SetFingerprint(local[0].ID(), recorded)
	push := PlannedResource{
		ID: testResourceID(), Action: ActionUpdateServer,
		Local: withTool("local", local[0].Attachment), Server: withTool("server", serverCopy),
	}
	push.changedAttachments = changedAttachmentIDs(push)
	require.Equal(t, []ResourceID{local[0].ID()}, push.changedAttachments)
	resources := []PlannedResource{push}

	require.NoError(t, checkAttachmentBaselines(root, resources, lock))

	assert.Equal(t, ActionUpdateServer, resources[0].Action, "the mapped description did not change in LaunchDarkly")
}

func TestBlockUnsupportedFieldsReportsTheSameFieldEveryTime(t *testing.T) {
	projection := syncdomain.ToolProjection{FieldStates: map[syncdomain.Field]syncdomain.FieldState{
		syncdomain.FieldSchema:      {Capability: syncdomain.CapabilityReadOnly, Reason: "schema is dynamic"},
		syncdomain.FieldDescription: {Capability: syncdomain.CapabilityReadOnly, Reason: "description is dynamic"},
	}}
	for range 50 {
		resource := PlannedResource{
			ID:              syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"},
			Action:          ActionUpdateServer,
			Source:          &syncdomain.Reference{File: "tool.json", Format: "json"},
			LocalProjection: projection,
		}

		blockUnsupportedFields(&resource)

		require.NotNil(t, resource.Problem)
		assert.Equal(t, `field "description" is read-only: description is dynamic`, resource.Problem.Message)
	}
}

func TestSourcePullStopsWhenLaunchDarklyHasNoValueForAMappedField(t *testing.T) {
	description := "Local"
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}
	resource := PlannedResource{
		ID:     syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"},
		Action: ActionUpdateLocal,
		Source: &syncdomain.Reference{File: "tool.json", Format: "json"},
		LocalProjection: syncdomain.ToolProjection{
			Description: &description,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldDescription: direct},
		},
		ServerProjection: syncdomain.ToolProjection{
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldDescription: direct},
		},
	}

	_, err := sourcePullReplacement(resource)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorSourceNotWritable, problem.Code)
	assert.Contains(t, problem.Message, `mapped field "description"`)
}

func TestAllowRelinkKeepsTheStalePinRefresh(t *testing.T) {
	local := testVariation("same")
	server := local
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionError, Local: &local, Server: &server,
		Source:              &syncdomain.Reference{File: "agents.py", Format: "python"},
		BaselineFingerprint: "same", LocalFingerprint: "same", ServerFingerprint: "same",
		Problem:                      syncdomain.NewProblem(syncdomain.ErrorBindingStale, "stale", ""),
		ServerHasStaleAttachmentPins: true,
	}
	resources := []PlannedResource{resource}

	allowRelink(resources, resource.ID)

	assert.Nil(t, resources[0].Problem)
	assert.Equal(t, ActionUpdateServer, resources[0].Action, "the relink still repins LaunchDarkly")
}

// A variation sync records, for each linked tool or skill that the variation
// uses, the content that LaunchDarkly has, not the repository copy, so that a
// local edit that no row synced stays a local change.
func TestRecordAttachmentStateKeepsAnUnsyncedLinkedEditLocal(t *testing.T) {
	root := t.TempDir()
	writeTestDescriptor := func(relativePath, content string) {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	writeTestDescriptor("tool.json", `{"description":"Edited locally","schema":{"type":"object"}}`)
	writeTestDescriptor(".launchdarkly/production/tools/search.ref.yaml",
		"formatVersion: 1\nfile: tool.json\nformat: json\nselect: $\nmap:\n  description: description\n")
	local, err := synclocal.CompileAttachments(root, func(ResourceID) bool { return true })
	require.NoError(t, err)
	server := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Version: 3, Tool: &syncdomain.Tool{
		Key: "search", Description: pointerTo("In LaunchDarkly"), Schema: map[string]any{"type": "object"},
	}}
	expected, err := syncdomain.SyncedAttachment{ProjectKey: "production", Attachment: server, Source: local[0].Source}.Fingerprint()
	require.NoError(t, err)
	manifest := syncmanifest.New()

	require.NoError(t, recordAttachmentState(&manifest, root, "production", []syncdomain.Attachment{server}))

	index := slices.IndexFunc(manifest.Resources, func(resource syncmanifest.Resource) bool { return resource.ID() == local[0].ID() })
	require.GreaterOrEqual(t, index, 0)
	assert.Equal(t, expected, manifest.Resources[index].Fingerprint, "the baseline is the LaunchDarkly content")
}

// A conflict over a shared tool survives the sync setting of the variation,
// and a choice that the setting forbids is an error, not a write.
func TestAttachmentConflictSurvivesASyncSetting(t *testing.T) {
	local := testVariation("same")
	server := local
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionConflict, Local: &local, Server: &server,
		Source: &syncdomain.Reference{File: "agents.py", Format: "python"}, attachmentConflict: true,
	}

	enforceDirection(&resource, syncdomain.SyncDirectionServerCanonical)
	assert.Equal(t, ActionConflict, resource.Action, "the setting does not resolve a shared-attachment conflict")

	resolved := applyConflictResolutions(Plan{Resources: []PlannedResource{resource}}, map[ResourceID]ConflictResolution{
		resource.ID: ConflictUseLocal,
	})
	require.Len(t, resolved.Resources, 1)
	assert.Equal(t, ActionError, resolved.Resources[0].Action)
	assert.Contains(t, resolved.Resources[0].Error, "cannot push")

	resolved = applyConflictResolutions(Plan{Resources: []PlannedResource{resource}}, map[ResourceID]ConflictResolution{
		resource.ID: ConflictUseLaunchDarkly,
	})
	assert.Equal(t, ActionUpdateLocal, resolved.Resources[0].Action, "the side that the setting allows applies")
}

func TestSourcePushStopsWhenTheSourceHasNoValueForAMappedField(t *testing.T) {
	description := "Server"
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}
	resource := PlannedResource{
		ID:     syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"},
		Action: ActionUpdateServer,
		Source: &syncdomain.Reference{File: "tool.json", Format: "json"},
		LocalProjection: syncdomain.ToolProjection{
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldDescription: {Capability: syncdomain.CapabilityDirect},
			},
		},
		ServerProjection: syncdomain.ToolProjection{
			Description: &description,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldDescription: direct},
		},
	}

	err := checkSourcePushClears(resource)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, syncdomain.ErrorSourceNotWritable, problem.Code)
	assert.Equal(t, `the source has no value for mapped field "description", so the push cannot clear it`, problem.Message)
	assert.Equal(t, "tool.json", problem.File)

	local := "Local"
	resource.LocalProjection = syncdomain.ToolProjection{
		Description: &local,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldDescription: direct},
	}
	assert.NoError(t, checkSourcePushClears(resource), "a source with a value for every mapped field pushes")
}

func TestConflictResolvedToLaunchDarklyNeedsDetachForALinkedResource(t *testing.T) {
	local := testVariation("local")
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionConflict, Local: &local,
		Source: &syncdomain.Reference{File: "agents.py", Format: "python"},
	}

	resolved := applyConflictResolutions(Plan{Resources: []PlannedResource{resource}}, map[ResourceID]ConflictResolution{
		resource.ID: ConflictUseLaunchDarkly,
	})

	require.Len(t, resolved.Resources, 1)
	assert.Equal(t, ActionError, resolved.Resources[0].Action, "a linked resource is detached, not deleted")
	require.NotNil(t, resolved.Resources[0].Problem)
	assert.Equal(t, syncdomain.ErrorResourceDeleted, resolved.Resources[0].Problem.Code)
	assert.Contains(t, resolved.Resources[0].Problem.Suggestion, "detach")
}

func TestPushClearCheckIsAPlanProblem(t *testing.T) {
	description := "Server"
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}
	resources := []PlannedResource{{
		ID:     syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"},
		Action: ActionUpdateServer,
		Source: &syncdomain.Reference{File: "tool.json", Format: "json"},
		LocalProjection: syncdomain.ToolProjection{
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{
				syncdomain.FieldDescription: {Capability: syncdomain.CapabilityDirect},
			},
		},
		ServerProjection: syncdomain.ToolProjection{
			Description: &description,
			FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldDescription: direct},
		},
	}}

	checkSourcePublications(t.TempDir(), resources)

	assert.Equal(t, ActionError, resources[0].Action, "the plan shows the problem before the review")
	require.NotNil(t, resources[0].Problem)
	assert.Equal(t, syncdomain.ErrorSourceNotWritable, resources[0].Problem.Code)
}

func TestLockRecordsLinkOnlyAfterTheLinkedResourceSyncs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, syncdomain.RootDir), 0o755))
	workspace := syncWorkspace{root: root, local: synclocal.NewStore(root)}
	id := syncdomain.VariationID("project", "support", "default")
	reference := syncdomain.Reference{File: "prompt.md", Format: "markdown", Select: "$", Map: map[string]string{"prompt": "body"}}
	prepared := synclink.Prepared{ID: id, Reference: reference}

	assert.False(t, lockRecordsLink(workspace, prepared), "an empty lock does not record the link")

	lock := syncmanifest.Manifest{Resources: []syncmanifest.Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "support/default",
		Fingerprint: "sha256:" + strings.Repeat("a", 64),
	}}}
	require.NoError(t, syncmanifest.WriteLock(workspace.local, lock))
	assert.False(t, lockRecordsLink(workspace, prepared), "a lock without the descriptor does not record the link")

	require.NoError(t, lock.SetSource(id, &reference, "markdown/1", nil))
	require.NoError(t, syncmanifest.WriteLock(workspace.local, lock))
	assert.True(t, lockRecordsLink(workspace, prepared), "a lock with the descriptor digest records the link")
}

func TestSourcePullGroupsJoinTwoNamesOfOneFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "prompts.json"), []byte(`{"a": "1", "b": "2"}`), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(root, "lib"), filepath.Join(root, "linked")))
	resources := []PlannedResource{
		{ID: syncdomain.VariationID("project", "support", "a"), Action: ActionUpdateLocal,
			Source: &syncdomain.Reference{File: "lib/prompts.json", Format: "json"}},
		{ID: syncdomain.VariationID("project", "support", "b"), Action: ActionUpdateLocal,
			Source: &syncdomain.Reference{File: "linked/prompts.json", Format: "json"}},
		{ID: syncdomain.VariationID("project", "support", "c"), Action: ActionUpdateLocal,
			Source: &syncdomain.Reference{File: "other.json", Format: "json"}},
	}

	groups := sourcePullGroups(root, resources)

	require.Len(t, groups, 2, "two names of one file form one group")
	assert.ElementsMatch(t, []int{0, 1}, groups[0])
	assert.Equal(t, []int{2}, groups[1])
}

func TestRecordAttachmentStateLeavesTheManifestWhenALinkedToolDoesNotCompile(t *testing.T) {
	root := t.TempDir()
	writeTestDescriptor := filepath.Join(root, syncdomain.RootDir, "project", "tools", "search.ref.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(writeTestDescriptor), 0o755))
	require.NoError(t, os.WriteFile(writeTestDescriptor, []byte("invalid: [\n"), 0o644))
	manifest := syncmanifest.Manifest{}
	tool := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Schema: map[string]any{"type": "object"},
	}}

	err := recordAttachmentState(&manifest, root, "project", []syncdomain.Attachment{tool})

	require.Error(t, err)
	assert.Empty(t, manifest.Resources, "a compile error records nothing")
}

func TestCodeCanonicalLinkedResourceDeletedInLaunchDarklyAsksForDetach(t *testing.T) {
	local := testVariation("local")
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionConflict, Local: &local,
		Source: &syncdomain.Reference{File: "agents.py", Format: "python"},
	}

	enforceDirection(&resource, syncdomain.SyncDirectionCodeCanonical)

	assert.Equal(t, ActionError, resource.Action)
	require.NotNil(t, resource.Problem, "the repository has a value, and LaunchDarkly deleted the resource")
	assert.Equal(t, syncdomain.ErrorResourceDeleted, resource.Problem.Code)
	assert.Contains(t, resource.Problem.Suggestion, "detach")
}

func TestLinkedResourceDeletedInLaunchDarklyWithLocalChangesAsksForDetachUnderBoth(t *testing.T) {
	local := testVariation("local")
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionConflict, Local: &local,
		Source: &syncdomain.Reference{File: "agents.py", Format: "python"},
	}

	enforceDirection(&resource, syncdomain.SyncDirectionBoth)

	assert.Equal(t, ActionError, resource.Action, "the conflict does not stay a conflict that a choice turns into a create")
	require.NotNil(t, resource.Problem)
	assert.Equal(t, syncdomain.ErrorResourceDeleted, resource.Problem.Code)
	assert.Contains(t, resource.Problem.Suggestion, "detach")
}

func TestVerifyLocalResultComparesTheProjectionOfALinkedVariation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "agents.py"), []byte("from agents import Agent\nsupport = Agent(instructions=\"Local text\")\n"), 0o644))
	id := syncdomain.VariationID("production", "support", "default")
	_, err := synclocal.NewStore(root).WriteDescriptor(id, syncdomain.Reference{
		File: "agents.py", Format: "python", Select: `agents[id="support"]`, Map: map[string]string{"prompt": "instructions"},
	})
	require.NoError(t, err)
	compiled, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	resource := func(text string) PlannedResource {
		server := testVariation("Server")
		server.Instructions = text
		projection, err := syncdomain.ProjectVariation(server, *compiled[0].Source)
		require.NoError(t, err)
		fingerprint, err := syncdomain.FingerprintProjection(id, projection)
		require.NoError(t, err)
		return PlannedResource{
			ID: id, Action: ActionUpdateLocal, Source: compiled[0].Source, Server: &server,
			ServerMode: syncdomain.VariationModeAgent, ServerProjection: projection, ServerFingerprint: fingerprint,
		}
	}

	require.NoError(t, verifyLocalResult(root, resource("Local text")), "the source holds the mapped fields of LaunchDarkly")
	err = verifyLocalResult(root, resource("Other text"))
	require.ErrorContains(t, err, "did not match the expected state", "a source without the pulled text is not in sync")
}

func TestServerCanonicalLinkedResourceDeletedInLaunchDarklyAsksForDetach(t *testing.T) {
	local := testVariation("local")
	resource := PlannedResource{
		ID: testResourceID(), Action: ActionConflict, Local: &local,
		Source: &syncdomain.Reference{File: "agents.py", Format: "python"},
	}

	enforceDirection(&resource, syncdomain.SyncDirectionServerCanonical)

	assert.Equal(t, ActionError, resource.Action)
	require.NotNil(t, resource.Problem, "LaunchDarkly deleted the resource, so the recovery is detach")
	assert.Equal(t, syncdomain.ErrorResourceDeleted, resource.Problem.Code)
}

func TestSourceLinkValidationNamesTheRealFileAndTheFlag(t *testing.T) {
	action := SourceLinkAction{
		ID:   syncdomain.VariationID("production", "support", "default"),
		File: "agents.py", Format: "python", Select: "agents[", Map: map[string]string{"prompt": "instructions"},
	}

	err := action.validate(syncdomain.ScopePrompts)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok)
	assert.Equal(t, "agents.py", problem.File, "the problem names the --file value")
	assert.Contains(t, problem.Message, `select "agents["`, "the problem names the part of the reference that failed")
}

func TestRecordSuccessfulChangeFailsWhenTheBindingCannotBeRecorded(t *testing.T) {
	manifest := syncmanifest.Manifest{Resources: []syncmanifest.Resource{{
		ResourceKind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/default",
		Fingerprint: "sha256:" + strings.Repeat("a", 64),
	}}}
	resource := PlannedResource{
		ID: syncdomain.VariationID("project", "config", "default"), Action: ActionUpdateServer,
		LocalFingerprint: "sha256:" + strings.Repeat("b", 64),
		Source:           &syncdomain.Reference{File: "prompt.txt", Format: "no-such-format"},
	}

	err := recordSuccessfulChange(&manifest, resource)

	require.Error(t, err, "a binding that cannot be recorded fails the row")
}

func TestMissingDescriptorOfALinkedResourceIsAProblemNotARestore(t *testing.T) {
	id := testResourceID()
	server := testVariation("server")
	manifest := syncmanifest.Manifest{Resources: []syncmanifest.Resource{{
		ResourceKind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey,
		Fingerprint: "sha256:" + strings.Repeat("a", 64), FingerprintSchema: syncdomain.ProjectionFingerprintSchema,
		ReferenceDigest: "sha256:" + strings.Repeat("b", 64), AdapterBehavior: "markdown/1",
	}}}

	plan := BuildPlan(manifest, nil, map[ResourceID]ServerResource{
		id: {Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
	})

	require.Len(t, plan.Resources, 1)
	assert.Equal(t, ActionError, plan.Resources[0].Action, "a missing descriptor is not restored as a managed file")
	require.NotNil(t, plan.Resources[0].Problem)
	assert.Equal(t, syncdomain.ErrorLinkedSourceMissing, plan.Resources[0].Problem.Code)
	assert.Contains(t, plan.Resources[0].Problem.Suggestion, "detach")
}

func TestLinkedAttachmentPushKeepsItsProjection(t *testing.T) {
	description, serverDescription := "Local", "Server"
	direct := syncdomain.FieldState{Capability: syncdomain.CapabilityDirect, Known: true}
	projection := syncdomain.ToolProjection{
		Description: &description,
		FieldStates: map[syncdomain.Field]syncdomain.FieldState{syncdomain.FieldDescription: direct},
	}
	server := syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{
		Key: "search", Description: &serverDescription, Schema: map[string]any{"type": "object"}, Tags: []string{"kept"},
	}}
	resource := PlannedResource{
		ID:               syncdomain.ResourceID{Kind: syncdomain.KindTool, ProjectKey: "production", LookupKey: "search"},
		Action:           ActionUpdateServer,
		Source:           &syncdomain.Reference{File: "tool.json", Format: "json"},
		LocalAttachment:  &syncdomain.Attachment{Kind: syncdomain.AttachmentTool, Tool: &syncdomain.Tool{Key: "search"}},
		ServerAttachment: &server,
		LocalProjection:  projection,
		ServerProjection: syncdomain.ToolProjection{Description: &serverDescription, FieldStates: projection.FieldStates},
	}

	local, err := attachmentToPublish(resource)

	require.NoError(t, err)
	assert.Equal(t, "Local", *local.Tool.Description, "the mapped field comes from the source")
	assert.Equal(t, []string{"kept"}, local.Tool.Tags, "an unowned field comes from LaunchDarkly")
	assert.Equal(t, projection, local.Projection, "the publish merges the projection onto the latest copy")
}
