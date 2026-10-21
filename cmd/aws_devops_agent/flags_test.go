package awsdevopsagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupOptionsRecordsAuthFlow(t *testing.T) {
	cmd := NewSetupCmd(nil)
	opts, err := setupOptions(cmd)
	require.NoError(t, err)
	assert.False(t, opts.AuthFlowSet)

	require.NoError(t, cmd.Flags().Set(authFlowFlag, "iam"))
	opts, err = setupOptions(cmd)
	require.NoError(t, err)
	assert.True(t, opts.AuthFlowSet)
}
