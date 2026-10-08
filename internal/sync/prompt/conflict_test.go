package prompt

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

func TestResolvedConflictAction(t *testing.T) {
	local := testVariation("local")
	server := testVariation("server")

	tests := []struct {
		name       string
		resource   PlannedResource
		resolution conflictResolution
		expected   Action
	}{
		{"LaunchDarkly updates an existing local resource", PlannedResource{Local: &local, Server: &server}, useLaunchDarkly, ActionUpdateLocal},
		{"LaunchDarkly restores a missing local resource", PlannedResource{Server: &server}, useLaunchDarkly, ActionUpdateLocal},
		{"LaunchDarkly deletion removes the local resource", PlannedResource{Local: &local}, useLaunchDarkly, ActionDeleteLocal},
		{"local updates an existing server resource", PlannedResource{Local: &local, Server: &server}, useLocal, ActionUpdateServer},
		{"local creates a missing server resource", PlannedResource{Local: &local}, useLocal, ActionCreateServer},
		{"local deletion archives the server resource", PlannedResource{Server: &server}, useLocal, ActionArchiveServer},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, resolvedConflictAction(test.resource, test.resolution))
		})
	}
}

func TestApplyConflictResolutionsDoesNotChangeReviewedPlan(t *testing.T) {
	id := testResourceID()
	sharedID := id
	sharedID.LookupKey = "config/shared"
	local, server := testVariation("local"), testVariation("server")
	reviewed := Plan{Resources: []PlannedResource{
		{ID: id, Action: ActionConflict, Local: &local, Server: &server},
		{ID: sharedID, Action: ActionUpdateServer, Local: &local, Server: &server},
	}}

	resolved := applyConflictResolutions(reviewed, map[ResourceID]conflictResolution{id: useLaunchDarkly, sharedID: useLaunchDarkly})

	assert.Equal(t, ActionConflict, reviewed.Resources[0].Action)
	assert.Equal(t, ActionUpdateServer, reviewed.Resources[1].Action)
	assert.Equal(t, ActionUpdateLocal, resolved.Resources[0].Action)
	assert.Equal(t, ActionUpdateLocal, resolved.Resources[1].Action)
}

func TestApplyLocalChangeRestoresMissingConflictFile(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	oldDescription := "Old local content"
	existing := testVariation("Existing")
	existing.Key = "existing"
	existing.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	existing.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{
			Key:         "search",
			Description: &oldDescription,
			Schema:      map[string]any{"type": "object"},
		},
	}}
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "production",
		ConfigKey:  "support",
		Variation:  existing,
	}})
	require.NoError(t, err)

	serverDescription := "Current server content"
	server := testVariation("server")
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
	server.Attachments = []syncdomain.Attachment{{
		Kind:    syncdomain.AttachmentTool,
		Version: 2,
		Tool: &syncdomain.Tool{
			Key:         "search",
			Description: &serverDescription,
			Schema:      map[string]any{"type": "object"},
		},
	}}
	resource := PlannedResource{
		ID:     testResourceID(),
		Action: ActionUpdateLocal,
		Server: &server,
	}

	require.NoError(t, applyLocalChange(store, resource))

	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 2)
	var restored syncdomain.SyncedResource
	for _, localResource := range resources {
		if localResource.LookupKey == resource.ID.LookupKey {
			restored = localResource
		}
	}
	assert.True(t, restored.Upsert)
	require.Len(t, restored.Attachments, 1)
	assert.Equal(t, serverDescription, *restored.Attachments[0].Tool.Description)
}

func TestWriteConflictChoice(t *testing.T) {
	var output bytes.Buffer
	writeConflictChoice(&output, conflictChoice{resolution: useLocal})
	writeConflictChoice(&output, conflictChoice{aborted: true})
	assert.Equal(t, "Using local.\nSync canceled; conflict left unresolved.\n", output.String())
}

func TestResolveConflictsShowsDiffBeforePrompt(t *testing.T) {
	plan := divergentPlan(t)
	var output bytes.Buffer
	input := strings.NewReader("\x1b[B\r")
	options := Options{Input: input, ErrorOutput: &output}

	result, err := resolveConflicts(options, plan, input, true, nil)

	require.NoError(t, err)
	assert.False(t, result.aborted)
	assert.Equal(t, useLocal, result.resolutions[testResourceID()])
	rendered := output.String()
	assert.Contains(t, rendered, "LaunchDarkly now")
	assert.Contains(t, rendered, "Local file now")
	assert.Less(t, strings.Index(rendered, "LaunchDarkly now"), strings.Index(rendered, "Using local."))
}

func TestResolveConflictsRequiresTerminalEvenWithYes(t *testing.T) {
	plan := divergentPlan(t)
	input := strings.NewReader("")

	_, err := resolveConflicts(Options{Input: input, ErrorOutput: &bytes.Buffer{}, Yes: true}, plan, input, false, nil)

	require.ErrorContains(t, err, "interactive conflict resolution requires a terminal")
}

func TestGroupConflictsDeduplicatesSharedAttachment(t *testing.T) {
	server := testVariation("support")
	local := server
	server.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	local.Tools = []syncdomain.AttachmentRef{{Key: "search"}}
	server.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}},
	}}
	local.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool,
		Tool: &syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "string"}},
	}}
	local.Name = "Locally renamed"
	first := PlannedResource{
		ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/first"},
		Action: ActionConflict, Local: &local, Server: &server, Diff: variationDiff(&server, &local),
	}
	first.changedAttachments = changedAttachmentIDs(first)
	second := first
	second.ID.LookupKey = "config/second"

	groups := groupConflicts(Plan{Resources: []PlannedResource{first, second}})

	require.Len(t, groups, 1)
	assert.True(t, groups[0].attachment)
	assert.Len(t, groups[0].resources, 2)
}

func TestGroupConflictsConnectsMixedChangesAcrossSharedAttachments(t *testing.T) {
	tool := attachmentID{projectKey: "project", kind: syncdomain.AttachmentTool, key: "search"}
	skill := attachmentID{projectKey: "project", kind: syncdomain.AttachmentSkill, key: "support"}
	resources := []PlannedResource{
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/first"},
			Action: ActionConflict, changedAttachments: []attachmentID{tool},
		},
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/second"},
			Action: ActionConflict, changedAttachments: []attachmentID{tool, skill},
		},
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/third"},
			Action: ActionConflict, changedAttachments: []attachmentID{skill},
		},
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/fourth"},
			Action: ActionUpdateServer, changedAttachments: []attachmentID{skill},
		},
		{
			ID:     ResourceID{Kind: syncdomain.KindVariation, ProjectKey: "project", LookupKey: "config/error"},
			Action: ActionError, changedAttachments: []attachmentID{skill},
		},
	}

	groups := groupConflicts(Plan{Resources: resources})

	require.Len(t, groups, 1)
	assert.True(t, groups[0].attachment)
	assert.Len(t, groups[0].resources, 4)
}

func TestRunWorkspaceSyncAppliesConflictChoiceAfterRevalidation(t *testing.T) {
	root := t.TempDir()
	baseline, local, server := testVariation("baseline"), testVariation("local"), testVariation("server")
	localStore, manifestStore := writeConflictWorkspace(t, root, baseline, local)
	api := &conflictAPI{variation: &server}
	runner := NewRunner(api)
	input := strings.NewReader("\x1b[B\r")
	runner.isTerminal = func(actual io.Reader, _ io.Writer) bool { return actual == input }
	var output bytes.Buffer

	err := runner.runWorkspaceSync(
		Options{
			AccessToken: "token", BaseURI: "https://example.test", Yes: true, Input: input,
			Output: &output, ErrorOutput: &output,
		},
		syncWorkspace{root: root, local: localStore, manifest: manifestStore},
	)

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, local.Name, api.variation.Name)
	assert.Contains(t, api.requests, "PATCH")
}

func TestRunWorkspaceSyncAbortsConflictWithoutWriting(t *testing.T) {
	root := t.TempDir()
	baseline, local, server := testVariation("baseline"), testVariation("local"), testVariation("server")
	localStore, manifestStore := writeConflictWorkspace(t, root, baseline, local)
	api := &conflictAPI{variation: &server}
	runner := NewRunner(api)
	input := strings.NewReader("\x1b[B\x1b[B\r")
	runner.isTerminal = func(actual io.Reader, _ io.Writer) bool { return actual == input }
	var output bytes.Buffer

	err := runner.runWorkspaceSync(
		Options{
			AccessToken: "token", BaseURI: "https://example.test", Yes: true, Input: input,
			Output: &output, ErrorOutput: &output,
		},
		syncWorkspace{root: root, local: localStore, manifest: manifestStore},
	)

	require.NoError(t, err)
	require.NotNil(t, api.variation)
	assert.Equal(t, server.Name, api.variation.Name)
	assert.NotContains(t, api.requests, "PATCH")
	assert.Contains(t, output.String(), "Sync canceled; conflict left unresolved.")
}

func TestRunWorkspaceSyncAbortsAttachmentConflictWithoutWriting(t *testing.T) {
	root := t.TempDir()
	baseline, local, server := attachmentConflictVariations()
	localStore, manifestStore := writeConflictWorkspace(t, root, baseline, local)
	api := &conflictAPI{variation: &server, tool: server.Attachments[0]}
	runner := NewRunner(api)
	input := strings.NewReader("\x1b[B\x1b[B\r")
	runner.isTerminal = func(actual io.Reader, _ io.Writer) bool { return actual == input }
	var output bytes.Buffer

	err := runner.runWorkspaceSync(
		Options{
			AccessToken: "token", BaseURI: "https://example.test", Yes: true, Input: input,
			Output: &output, ErrorOutput: &output,
		},
		syncWorkspace{root: root, local: localStore, manifest: manifestStore},
	)

	require.NoError(t, err)
	assert.NotContains(t, api.requests, "PATCH")
	assert.Equal(t, "Changed in LaunchDarkly", *api.tool.Tool.Description)

	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.Len(t, resources[0].Attachments, 1)
	assert.Equal(t, "Changed locally", *resources[0].Attachments[0].Tool.Description)
	assert.Contains(t, output.String(), "Sync canceled; conflict left unresolved.")
}

func TestRunWorkspaceSyncUsesLaunchDarklyForAttachmentConflict(t *testing.T) {
	root := t.TempDir()
	baseline, local, server := attachmentConflictVariations()
	localStore, manifestStore := writeConflictWorkspace(t, root, baseline, local)
	api := &conflictAPI{variation: &server, tool: server.Attachments[0]}
	runner := NewRunner(api)
	input := strings.NewReader("\r")
	runner.isTerminal = func(actual io.Reader, _ io.Writer) bool { return actual == input }
	var output bytes.Buffer

	err := runner.runWorkspaceSync(
		Options{
			AccessToken: "token", BaseURI: "https://example.test", Yes: true, Input: input,
			Output: &output, ErrorOutput: &output,
		},
		syncWorkspace{root: root, local: localStore, manifest: manifestStore},
	)

	require.NoError(t, err)
	assert.NotContains(t, api.requests, "PATCH")
	assert.Equal(t, "Changed in LaunchDarkly", *api.tool.Tool.Description)

	resources, err := synclocal.CompileWorkspace(root)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	require.Len(t, resources[0].Attachments, 1)
	assert.Equal(t, "Changed in LaunchDarkly", *resources[0].Attachments[0].Tool.Description)
	assert.Contains(t, output.String(), "Using LaunchDarkly.")
}

func attachmentConflictVariations() (syncdomain.Variation, syncdomain.Variation, syncdomain.Variation) {
	description := "Baseline"
	tool := syncdomain.Tool{
		Key: "search", Description: &description, Schema: map[string]any{"type": "object"},
	}
	baseline := testVariation("support")
	baseline.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 1}}
	baseline.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool, Version: 1, Tool: &tool,
	}}

	local := baseline
	localTool := tool
	localDescription := "Changed locally"
	localTool.Description = &localDescription
	local.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool, Tool: &localTool,
	}}

	server := baseline
	serverTool := tool
	serverDescription := "Changed in LaunchDarkly"
	serverTool.Description = &serverDescription
	server.Tools = []syncdomain.AttachmentRef{{Key: "search", Version: 2}}
	server.Attachments = []syncdomain.Attachment{{
		Kind: syncdomain.AttachmentTool, Version: 2, Tool: &serverTool,
	}}
	return baseline, local, server
}

func divergentPlan(t *testing.T) Plan {
	t.Helper()

	id := testResourceID()
	baseline, local, server := testVariation("baseline"), testVariation("local"), testVariation("server")
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
	return BuildPlan(manifest, localResources(&local, true), map[ResourceID]ServerResource{
		id: {Variation: &server, ConfigMode: syncdomain.VariationModeAgent},
	})
}

func writeConflictWorkspace(t *testing.T, root string, baseline, local syncdomain.Variation) (synclocal.Store, syncmanifest.Store) {
	t.Helper()

	localStore := synclocal.NewStore(root)
	_, err := localStore.Add([]synclocal.VariationFile{{
		ProjectKey: "production", ConfigKey: "support", Upsert: true, Variation: local,
	}})
	require.NoError(t, err)

	id := testResourceID()
	fingerprint, err := syncdomain.FingerprintVariation(id.ProjectKey, id.LookupKey, baseline)
	require.NoError(t, err)
	manifestStore := syncmanifest.NewStore(root)
	require.NoError(t, manifestStore.Write(syncmanifest.Manifest{
		FormatVersion: syncmanifest.FormatVersion,
		Resources: []syncmanifest.Resource{{
			ResourceKind: id.Kind, ProjectKey: id.ProjectKey, LookupKey: id.LookupKey, Fingerprint: fingerprint,
		}},
	}))
	return localStore, manifestStore
}

type conflictAPI struct {
	variation *syncdomain.Variation
	tool      syncdomain.Attachment
	requests  []string
}

func (api *conflictAPI) MakeRequest(
	_ string,
	method string,
	path string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	api.requests = append(api.requests, method)
	if strings.Contains(path, "/ai-tools/") {
		if method == "PATCH" {
			var tool syncdomain.Tool
			if err := json.Unmarshal(body, &tool); err != nil {
				return nil, err
			}
			api.tool.Tool = &tool
			api.tool.Version++
		}
		return json.Marshal(struct {
			syncdomain.Tool
			Version int `json:"version"`
		}{Tool: *api.tool.Tool, Version: api.tool.Version})
	}
	if method == "GET" {
		return json.Marshal(map[string]any{
			"key": "support", "name": "Support", "mode": "agent", "variations": []syncdomain.Variation{*api.variation},
		})
	}
	if method == "PATCH" {
		var update syncdomain.Variation
		if err := json.Unmarshal(body, &update); err != nil {
			return nil, err
		}
		update.Mode = syncdomain.VariationModeAgent
		update.Key = api.variation.Key
		api.variation = &update
	}
	return []byte(`{}`), nil
}

func (*conflictAPI) MakeUnauthenticatedRequest(string, string, []byte) ([]byte, error) {
	return nil, nil
}
