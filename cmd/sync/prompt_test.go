package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
)

func TestPromptCommandDefinesSyncFlags(t *testing.T) {
	command := NewPromptCmd(nil)

	assert.Equal(t, "prompt", command.Use)
	for _, name := range []string{addFlag, applyFlag, dryRunFlag, yesFlag, cliflags.ProjectFlag} {
		assert.NotNil(t, command.Flags().Lookup(name), "missing --%s", name)
	}
}
