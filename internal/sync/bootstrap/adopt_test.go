package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

type settingsPatch struct {
	projectKey string
	upserts    []syncapi.SyncSettingRule
	deletions  []syncapi.SyncSettingPattern
}

type recordingSettings struct {
	patches []settingsPatch
	err     error
}

func (settings *recordingSettings) PatchSyncSettings(
	projectKey string,
	upserts []syncapi.SyncSettingRule,
	deletions []syncapi.SyncSettingPattern,
) error {
	settings.patches = append(settings.patches, settingsPatch{projectKey: projectKey, upserts: upserts, deletions: deletions})
	return settings.err
}

func adoptFiles() []synclocal.VariationFile {
	variation := func(key string) syncdomain.Variation {
		return syncdomain.Variation{Mode: syncdomain.VariationModeAgent, Key: key, Name: key, Instructions: "Be helpful."}
	}
	return []synclocal.VariationFile{
		{ProjectKey: "beta", ConfigKey: "support-bot", Upsert: true, Variation: variation("default")},
		{ProjectKey: "alpha", ConfigKey: "support-bot", Upsert: true, Variation: variation("default")},
		{ProjectKey: "alpha", ConfigKey: "support-bot", Upsert: true, Variation: variation("short")},
	}
}

func TestFinishSelectionAdoptsEachVariation(t *testing.T) {
	tests := []struct {
		direction syncdomain.SourceOfTruth
		summary   string
	}{
		{syncdomain.SourceOfTruthCodeOnly, "The local files are now the source of truth for the prompts of these variations."},
		{syncdomain.SourceOfTruthUIAndCode, "The LaunchDarkly UI and the local files can now both change these variations."},
	}
	for _, test := range tests {
		t.Run(string(test.direction), func(t *testing.T) {
			root := t.TempDir()
			var output bytes.Buffer
			settings := &recordingSettings{}

			err := finishSelection(Options{
				Store:         synclocal.NewStore(root),
				Baselines:     &memoryManifestStore{manifest: syncmanifest.New()},
				Settings:      settings,
				SourceOfTruth: test.direction,
				Output:        &output,
				Initial:       true,
			}, adoptFiles())

			require.NoError(t, err)
			rule := func(lookupKey string) syncapi.SyncSettingRule {
				id := syncdomain.ResourceID{Kind: syncdomain.KindVariation, LookupKey: lookupKey}
				return syncapi.ExactVariationRules([]syncdomain.ResourceID{id}, test.direction)[0]
			}
			assert.Equal(t, []settingsPatch{
				{projectKey: "alpha", upserts: []syncapi.SyncSettingRule{rule("support-bot/default"), rule("support-bot/short")}},
				{projectKey: "beta", upserts: []syncapi.SyncSettingRule{rule("support-bot/default")}},
			}, settings.patches)
			assert.Contains(t, output.String(), test.summary)
		})
	}
}

func TestRunNeedsSourceOfTruthWithNoInput(t *testing.T) {
	settings := &recordingSettings{}

	err := Run(Options{
		Store:      synclocal.NewStore(t.TempDir()),
		Settings:   settings,
		Output:     &bytes.Buffer{},
		NoInput:    true,
		Selections: []syncdomain.ResourceID{syncdomain.VariationID("alpha", "support-bot", "default")},
	})

	require.ErrorContains(t, err, "add needs a source of truth. Use --source-of-truth ui-and-code or code-only.")
	assert.Empty(t, settings.patches)
}

func TestFinishSelectionWithoutSettingsWriterWritesNoRule(t *testing.T) {
	var output bytes.Buffer

	err := finishSelection(Options{
		Store:     synclocal.NewStore(t.TempDir()),
		Baselines: &memoryManifestStore{manifest: syncmanifest.New()},
		Output:    &output,
		Initial:   true,
	}, adoptFiles())

	require.NoError(t, err)
	assert.NotContains(t, output.String(), "source of truth")
}

func TestFinishSelectionDryRunWritesNoRule(t *testing.T) {
	settings := &recordingSettings{}

	err := finishSelection(Options{
		Store:    synclocal.NewStore(t.TempDir()),
		Settings: settings,
		Output:   &bytes.Buffer{},
		DryRun:   true,
	}, adoptFiles())

	require.NoError(t, err)
	assert.Empty(t, settings.patches)
}

func TestFinishSelectionWarnsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	var output, errorOutput bytes.Buffer
	settings := &recordingSettings{err: fmt.Errorf("update sync settings: %w", syncapi.ErrSyncSettingsUnavailable)}

	err := finishSelection(Options{
		Store:         store,
		Baselines:     &memoryManifestStore{manifest: syncmanifest.New()},
		Settings:      settings,
		SourceOfTruth: syncdomain.SourceOfTruthCodeOnly,
		Output:        &output,
		ErrorOutput:   &errorOutput,
		Initial:       true,
	}, adoptFiles())

	require.NoError(t, err)
	assert.Len(t, settings.patches, 2)
	assert.Contains(t, errorOutput.String(), "Warning: sync settings are not available for project alpha.")
	assert.Contains(t, errorOutput.String(), "Warning: sync settings are not available for project beta.")
	assert.NotContains(t, output.String(), "source of truth")
	exists, err := store.VariationExists("alpha", "support-bot", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestFinishSelectionStopsBeforeFilesWhenRuleWriteFails(t *testing.T) {
	root := t.TempDir()
	store := synclocal.NewStore(root)
	settings := &recordingSettings{err: errors.New("boom")}

	err := finishSelection(Options{
		Store:         store,
		Baselines:     &memoryManifestStore{manifest: syncmanifest.New()},
		Settings:      settings,
		SourceOfTruth: syncdomain.SourceOfTruthCodeOnly,
		Output:        &bytes.Buffer{},
		Initial:       true,
	}, adoptFiles())

	require.ErrorContains(t, err, "boom")
	exists, err := store.Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}
