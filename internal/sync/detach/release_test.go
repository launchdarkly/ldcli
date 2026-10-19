package detach

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

type recordingSettings struct {
	projectKeys []string
	upserts     []syncapi.SyncSettingRule
	deletions   []syncapi.SyncSettingPattern
	err         error
}

func (settings *recordingSettings) PatchSyncSettings(
	projectKey string,
	upserts []syncapi.SyncSettingRule,
	deletions []syncapi.SyncSettingPattern,
) error {
	settings.projectKeys = append(settings.projectKeys, projectKey)
	settings.upserts = append(settings.upserts, upserts...)
	settings.deletions = append(settings.deletions, deletions...)
	return settings.err
}

func TestRunDeletesExactRuleOfEachDetachedVariation(t *testing.T) {
	workspace := newArchiveWorkspace(t, "first", "second")
	settings := &recordingSettings{}
	options := workspace.options(&recordingArchiver{})
	options.Settings = settings

	require.NoError(t, Run(options))

	assert.Equal(t, []string{"project"}, settings.projectKeys)
	assert.Empty(t, settings.upserts)
	assert.Equal(t, []syncapi.SyncSettingPattern{
		{ResourceKind: syncdomain.KindVariation, ResourcePattern: "config/first"},
		{ResourceKind: syncdomain.KindVariation, ResourcePattern: "config/second"},
	}, settings.deletions)
	workspace.requireTracked(t, "first", false)
	workspace.requireTracked(t, "second", false)
}

func TestRunDetachWarnsWhenSyncSettingsAreUnavailable(t *testing.T) {
	workspace := newArchiveWorkspace(t, "prompt")
	var errorOutput bytes.Buffer
	options := workspace.options(&recordingArchiver{})
	options.Settings = &recordingSettings{err: fmt.Errorf("update: %w", syncapi.ErrSyncSettingsUnavailable)}
	options.ErrorOutput = &errorOutput

	require.NoError(t, Run(options))

	assert.Contains(t, errorOutput.String(), "Warning: sync settings are not available for project project.")
	workspace.requireTracked(t, "prompt", false)
}

func TestRunDetachKeepsFilesWhenRuleDeleteFails(t *testing.T) {
	workspace := newArchiveWorkspace(t, "prompt")
	options := workspace.options(&recordingArchiver{})
	settings := &recordingSettings{err: errors.New("boom")}
	options.Settings = settings

	require.ErrorContains(t, Run(options), "boom")
	workspace.requireTracked(t, "prompt", true)

	settings.err = nil
	require.NoError(t, Run(options))
	workspace.requireTracked(t, "prompt", false)
}
