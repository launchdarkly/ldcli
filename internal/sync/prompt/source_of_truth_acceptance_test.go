package prompt_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Contains(t, stdout, "To let the LaunchDarkly UI change a prompt again, change the source of truth in the variation menu in LaunchDarkly.")
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
