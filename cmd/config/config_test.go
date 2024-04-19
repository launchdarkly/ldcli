package config_test

import (
	"ldcli/cmd"
	"ldcli/internal/analytics"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoFlag(t *testing.T) {
	expected, err := os.ReadFile("testdata/help.golden")
	require.NoError(t, err)
	args := []string{
		"config",
	}

	output, err := cmd.CallCmd(t, cmd.APIClients{}, &analytics.NoopClient{}, args)

	require.NoError(t, err)

	assert.Equal(t, string(expected), string(output))
}
