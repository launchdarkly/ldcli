package prompt

import (
	"bufio"
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
	local, server := testVariation("local"), testVariation("server")
	reviewed := Plan{Resources: []PlannedResource{{
		ID: id, Action: ActionConflict, Local: &local, Server: &server,
	}}}

	resolved := applyConflictResolutions(reviewed, map[ResourceID]conflictResolution{id: useLocal})

	assert.Equal(t, ActionConflict, reviewed.Resources[0].Action)
	assert.Equal(t, ActionUpdateServer, resolved.Resources[0].Action)
}

func TestPromptConflictResolution(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		expectedResolution conflictResolution
		expectedAbort      bool
	}{
		{"LaunchDarkly number", "1\n", useLaunchDarkly, false},
		{"LaunchDarkly name", "launchdarkly\n", useLaunchDarkly, false},
		{"server alias", "server\n", useLaunchDarkly, false},
		{"local number", "2\n", useLocal, false},
		{"local name", "local\n", useLocal, false},
		{"abort number", "3\n", "", true},
		{"abort name", "abort\n", "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			resolution, aborted, err := promptConflictResolution(bufio.NewReader(strings.NewReader(test.input)), &output)

			require.NoError(t, err)
			assert.Equal(t, test.expectedResolution, resolution)
			assert.Equal(t, test.expectedAbort, aborted)
		})
	}
}

func TestPromptConflictResolutionRetriesInvalidChoice(t *testing.T) {
	var output bytes.Buffer

	resolution, aborted, err := promptConflictResolution(bufio.NewReader(strings.NewReader("other\n2\n")), &output)

	require.NoError(t, err)
	assert.Equal(t, useLocal, resolution)
	assert.False(t, aborted)
	assert.Contains(t, output.String(), "Enter 1, 2, or 3.")
}

func TestPromptConflictResolutionIgnoresBlankInputWithoutRepeatingMenu(t *testing.T) {
	var output bytes.Buffer

	resolution, aborted, err := promptConflictResolution(bufio.NewReader(strings.NewReader("\n\n2\n")), &output)

	require.NoError(t, err)
	assert.Equal(t, useLocal, resolution)
	assert.False(t, aborted)
	assert.Equal(t, 1, strings.Count(output.String(), "Choose how to resolve this conflict"))
	assert.NotContains(t, output.String(), "Enter 1, 2, or 3.")
}

func TestConflictChoiceAndConfirmationShareInput(t *testing.T) {
	var output bytes.Buffer
	input := bufio.NewReader(strings.NewReader("2\ny\n"))

	resolution, aborted, err := promptConflictResolution(input, &output)
	require.NoError(t, err)
	assert.Equal(t, useLocal, resolution)
	assert.False(t, aborted)

	confirmed, err := confirmApply(input, &output, true)
	require.NoError(t, err)
	assert.True(t, confirmed)
}

func TestResolveConflictsShowsDiffBeforePrompt(t *testing.T) {
	plan := divergentPlan(t)
	var output bytes.Buffer
	input := bufio.NewReader(strings.NewReader("2\n"))
	options := Options{Input: input, ErrorOutput: &output}

	result, err := resolveConflicts(options, plan, input, true, nil)

	require.NoError(t, err)
	assert.False(t, result.aborted)
	assert.Equal(t, useLocal, result.resolutions[testResourceID()])
	rendered := output.String()
	assert.Contains(t, rendered, "LaunchDarkly now")
	assert.Contains(t, rendered, "Local file now")
	assert.Less(t, strings.Index(rendered, "LaunchDarkly now"), strings.Index(rendered, "Choose how to resolve"))
}

func TestResolveConflictsRequiresTerminalEvenWithYes(t *testing.T) {
	plan := divergentPlan(t)
	input := bufio.NewReader(strings.NewReader("2\n"))

	_, err := resolveConflicts(Options{Input: input, ErrorOutput: &bytes.Buffer{}, Yes: true}, plan, input, false, nil)

	require.ErrorContains(t, err, "interactive conflict resolution requires a terminal")
}

func TestRunWorkspaceSyncAppliesConflictChoiceAfterRevalidation(t *testing.T) {
	root := t.TempDir()
	baseline, local, server := testVariation("baseline"), testVariation("local"), testVariation("server")
	localStore, manifestStore := writeConflictWorkspace(t, root, baseline, local)
	api := &conflictAPI{variation: &server}
	runner := NewRunner(api)
	input := strings.NewReader("2\ny\n")
	runner.isTerminal = func(actual io.Reader, _ io.Writer) bool { return actual == input }
	var output bytes.Buffer

	err := runner.runWorkspaceSync(
		Options{
			AccessToken: "token", BaseURI: "https://example.test", Input: input,
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
	input := strings.NewReader("3\n")
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
	requests  []string
}

func (api *conflictAPI) MakeRequest(
	_ string,
	method string,
	_ string,
	_ string,
	_ url.Values,
	body []byte,
	_ bool,
) ([]byte, error) {
	api.requests = append(api.requests, method)
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
