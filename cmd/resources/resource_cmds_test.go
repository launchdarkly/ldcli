package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestKebabCaseQueryParamConversion(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{"items": []}`),
	}

	t.Run("converts kebab-case flag --with-branches to camelCase query param withBranches", func(t *testing.T) {
		args := []string{
			"code-refs", "list-repositories",
			"--access-token", "abcd1234",
			"--with-branches", "true",
		}

		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient: mockClient,
			},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Equal(t, "true", mockClient.Query.Get("withBranches"), "query param should be camelCase withBranches, not kebab-case with-branches")
		assert.Empty(t, mockClient.Query.Get("with-branches"), "kebab-case with-branches should not appear in query")
	})
}

func TestCreateTeam(t *testing.T) {
	t.Run("help shows postTeam description", func(t *testing.T) {
		args := []string{
			"teams",
			"create",
			"--help",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

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

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		s := string(output)
		assert.Contains(t, s, "would be making a post request to /api/v2/teams here, with args: map[data:map[key:team-key name:Team Name] expand:]\n")
	})
}
