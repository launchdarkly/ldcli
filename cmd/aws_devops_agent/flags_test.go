package awsdevopsagent

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/analytics"
)

// Both flags must fail before any AWS call.
func TestSetupRejectsSpaceIDWithNewSpace(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	cmd := NewSetupCmd(analytics.NoopClientFn{}.Tracker())
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--agent-space-id", "space-2", "--new-agent-space", "--region", "us-east-1"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), agentSpaceIDFlag)
	assert.Contains(t, err.Error(), newAgentSpaceFlag)
	assert.NotContains(t, err.Error(), "AWS")
}

func TestSetupAcceptsEachSpaceFlagAlone(t *testing.T) {
	for _, args := range [][]string{{"--agent-space-id", "space-2"}, {"--new-agent-space"}} {
		cmd := NewSetupCmd(nil)
		require.NoError(t, cmd.ParseFlags(args))
		assert.NoError(t, cmd.ValidateFlagGroups())
	}
}
