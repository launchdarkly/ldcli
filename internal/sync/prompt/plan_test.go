package prompt

import (
	"slices"
	"strings"
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

	checkAttachmentBaselines(resources, lock)

	assert.Equal(t, ActionUpdateServer, resources[0].Action, "LaunchDarkly still has the recorded tool")
	assert.Equal(t, ActionConflict, resources[1].Action, "the pull would overwrite the local edit")
	assert.Equal(t, ActionUpdateServer, resources[2].Action, "LaunchDarkly has no copy of the tool on this variation")
	groups := groupConflicts(Plan{Resources: resources})
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].resources, 3, "one choice covers every variation that writes the tool")

	untracked := []PlannedResource{pushEdit}
	untracked[0].changedAttachments = changedAttachmentIDs(pushEdit)
	checkAttachmentBaselines(untracked, syncmanifest.New())
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
