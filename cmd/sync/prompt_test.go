package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptCommandDefinesSyncFlags(t *testing.T) {
	command := NewPromptCmd(nil)

	assert.Equal(t, "prompt", command.Use)
	for _, name := range []string{addFlag, detachFlag, dryRunFlag, formatFlag, linkFlag, watchFlag, yesFlag} {
		assert.NotNil(t, command.Flags().Lookup(name), "missing --%s", name)
	}
	assert.Nil(t, command.Flags().Lookup("apply"))
	assert.Nil(t, command.Flags().Lookup("project"))
}
