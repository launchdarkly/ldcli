package prompt_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

// The source of truth changes only in LaunchDarkly, so add writes no sync rule.
func TestPromptAddWritesNoRule(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--no-input")

	require.NoError(t, err)
	assert.Empty(t, api.settingsPatches)
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
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
