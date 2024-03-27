package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/flags"
)

func TestCreate(t *testing.T) {
	t.Run("with valid flags prints version", func(t *testing.T) {
		client := flags.MockClient{}
		args := []string{
			"--version",
		}

		output, err := CallCmd(t, &client, nil, nil, args)

		require.NoError(t, err)
		assert.Contains(t, string(output), `ldcli version test`)
	})
}
