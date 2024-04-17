package cmd_test

import (
	"ldcli/cmd"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate(t *testing.T) {
	t.Run("with valid flags prints version", func(t *testing.T) {
		args := []string{
			"--version",
		}

		output, err := cmd.CallCmd(t, cmd.Clients{}, args)

		require.NoError(t, err)
		assert.Contains(t, string(output), `ldcli version test`)
	})
}
