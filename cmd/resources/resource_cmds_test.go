package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/analytics"
)

func TestCreateTeam(t *testing.T) {
	t.Run("help shows postTeam description", func(t *testing.T) {
		args := []string{
			"teams",
			"post-team", // temporary command name
			"--help",
		}

		output, err := cmd.CallCmd(t, cmd.APIClients{}, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.Contains(t, string(output), "Create a team.")
	})
	t.Run("with valid flags calls makeRequest function", func(t *testing.T) {
		t.Skip("TODO: add back when mock client is added")
		args := []string{
			"teams",
			"post-team", // temporary command name
			"--access-token",
			"abcd1234",
			"--data",
			`{"key": "team-key", "name": "Team Name"}`,
		}

		output, err := cmd.CallCmd(t, cmd.APIClients{}, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		s := string(output)
		assert.Contains(t, s, "would be making a post request to /api/v2/teams here, with args: map[data:map[key:team-key name:Team Name] expand:]\n")
	})
}
