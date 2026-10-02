package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestPromptCommandDefinesSyncFlags(t *testing.T) {
	command := NewPromptCmd(nil)

	assert.Equal(t, "prompt", command.Use)
	for _, name := range []string{
		addFlag, attachSkillFlag, attachToolFlag, detachFlag, dryRunFlag, formatFlag,
		linkFlag, projectFlag, variationFlag, watchFlag, yesFlag,
	} {
		assert.NotNil(t, command.Flags().Lookup(name), "missing --%s", name)
	}
	assert.Nil(t, command.Flags().Lookup("apply"))
}

func TestAttachmentFlagAcceptsNoKeyForInteractiveSelection(t *testing.T) {
	command := NewPromptCmd(nil)

	require.NoError(t, command.ParseFlags([]string{"--attach-tool"}))

	request, err := attachmentRequest(command)
	require.NoError(t, err)
	require.NotNil(t, request)
	assert.Equal(t, syncdomain.AttachmentTool, request.Kind)
	assert.Empty(t, request.Key)
}
