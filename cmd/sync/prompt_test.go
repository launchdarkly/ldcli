package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptCommandDefinesDryRunFlag(t *testing.T) {
	command := NewPromptCmd(nil)
	assert.Equal(t, "prompt", command.Use)
	assert.NotNil(t, command.Flags().Lookup(dryRunFlag))
}
