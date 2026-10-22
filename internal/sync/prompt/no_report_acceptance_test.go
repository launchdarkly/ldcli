package prompt_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LaunchDarkly has no sync report endpoint, so a sync must not send one.
func TestPromptSyncSendsNoReport(t *testing.T) {
	root := initRepository(t)
	baseline, local := variation("Baseline"), variation("Local")
	writeVariation(t, root, local, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)

	assert.Empty(t, api.reports)
}
