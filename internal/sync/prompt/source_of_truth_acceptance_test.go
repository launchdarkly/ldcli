package prompt_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/sync/api/apitest"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

func TestPromptAddMakesVariationCodeOnly(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	stdout, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "code-only", "--no-input")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"code_canonical"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The local files are now the source of truth for the prompts of these variations.")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptAddWithUIAndCodeMakesVariationBoth(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	stdout, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "ui-and-code", "--no-input")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"both"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The LaunchDarkly UI and the local files can now both change these variations.")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptAddNeedsSourceOfTruth(t *testing.T) {
	tests := map[string][]string{
		"with no input":    {"--no-input"},
		"without terminal": {},
	}
	for name, flags := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{variation: pointer(variation("Server"))}

			_, _, err := runPrompt(t, root, api, append([]string{"add", "production/support/default"}, flags...)...)

			require.ErrorContains(t, err, "add needs a source of truth. Use --source-of-truth ui-and-code or code-only.")
			assert.Empty(t, api.settingsPatches)
			exists, err := synclocal.NewStore(root).Exists()
			require.NoError(t, err)
			assert.False(t, exists)
		})
	}
}

func TestPromptAddRejectsUnknownSourceOfTruth(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "launchdarkly", "--no-input")

	require.ErrorContains(t, err, `the source of truth must be ui-and-code or code-only, not "launchdarkly"`)
	assert.Empty(t, api.requests)
}

func TestPromptSourceOfTruthChangesRuleAndKeepsFile(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	stdout, _, err := runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"code_canonical"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The local files are now the source of truth for the prompts of these variations.")
	assert.Empty(t, api.reports, "source-of-truth does not sync")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptSourceOfTruthNeedsSyncedVariation(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "source-of-truth", "ui-and-code", "production/support/other")

	require.ErrorContains(t, err, "variation production/support/other does not sync in this workspace.")
	assert.Empty(t, api.settingsPatches)
}

func TestPromptSourceOfTruthRejectsDuplicateAndUnknownValue(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "source-of-truth", "both", "production/support/default")
	require.ErrorContains(t, err, `the source of truth must be ui-and-code or code-only, not "both"`)

	_, _, err = runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default", "production/support/default")
	require.ErrorContains(t, err, "variation production/support/default was selected more than once")
	assert.Empty(t, api.settingsPatches)
}

type failingProjectSettingsAPI struct {
	*directAPI
	projectKey string
}

func (api failingProjectSettingsAPI) serve(request apitest.Request) (int, []byte) {
	if request.Method == http.MethodPatch && strings.HasPrefix(request.Path, "/api/v2/projects/"+api.projectKey+"/configs/sync/settings") {
		return 500, []byte(`{"code":"internal","message":"boom","statusCode":500}`)
	}
	return api.directAPI.serve(request)
}

func TestPromptSourceOfTruthNamesChangedProjectsWhenLaterProjectFails(t *testing.T) {
	root := initRepository(t)
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{
		{ProjectKey: "production", ConfigKey: "support", Variation: variation("Production")},
		{ProjectKey: "staging", ConfigKey: "support", Variation: variation("Staging")},
	})
	require.NoError(t, err)
	api := failingProjectSettingsAPI{directAPI: &directAPI{}, projectKey: "staging"}

	_, stderr, err := runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default", "staging/support/default")

	require.ErrorContains(t, err, "boom")
	assert.Contains(t, stderr, "The source of truth changed for project production only.")
	assert.Len(t, api.settingsPatches, 1)
}

func TestPromptSourceOfTruthFailsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation:        pointer(baseline),
		settingsPatchErr: notFoundError,
	}

	_, _, err := runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default")

	require.ErrorContains(t, err, `update sync settings for project "production": sync settings are not available`)
}

func TestPromptSourceOfTruthOverridesWildcardWithUIAndCode(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	stdout, _, err := runPrompt(t, root, api, "source-of-truth", "ui-and-code", "production/support/default")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"both"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The LaunchDarkly UI and the local files can now both change these variations.")
}

func TestPromptAddWarnsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{
		variation:        pointer(variation("Server")),
		settingsPatchErr: notFoundError,
	}

	stdout, stderr, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "code-only", "--no-input")

	require.NoError(t, err)
	assert.Len(t, api.settingsPatches, 1)
	assert.Contains(t, stderr, "Warning: sync settings are not available for project production.")
	assert.NotContains(t, stdout, "source of truth")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptAddFailsWhenSyncSettingsAreForbidden(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{
		variation:        pointer(variation("Server")),
		settingsPatchErr: forbiddenError,
	}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "code-only", "--no-input")

	require.ErrorContains(t, err, "Access denied")
	exists, err := synclocal.NewStore(root).Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPromptAddDryRunWritesNoRule(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--no-input", "--dry-run")

	require.NoError(t, err)
	assert.Empty(t, api.settingsPatches)
	assert.Empty(t, api.reports)
}

func TestPromptDetachDeletesExactRule(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "detach", "production/support/default", "--no-input")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [],
		"deletions": [{"resourceKind":"variation","resourcePattern":"support/default"}]
	}`, api.settingsPatches[0])
	assert.Empty(t, api.reports, "detach does not sync")
}

func TestPromptDetachWarnsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation:        pointer(baseline),
		settingsPatchErr: notFoundError,
	}

	_, stderr, err := runPrompt(t, root, api, "detach", "production/support/default", "--no-input")

	require.NoError(t, err)
	assert.Contains(t, stderr, "Warning: sync settings are not available for project production.")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPromptDetachKeepsFilesWhenRuleDeleteFails(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation:        pointer(baseline),
		settingsPatchErr: boomError,
	}

	_, _, err := runPrompt(t, root, api, "detach", "production/support/default", "--no-input")

	require.ErrorContains(t, err, "boom")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}
