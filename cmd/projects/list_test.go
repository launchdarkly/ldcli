package projects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/errors"
	"ldcli/internal/projects"
)

func TestList(t *testing.T) {
	errorHelp := ". See `ldcli projects list --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
	}
	stubbedResponse := `{
		"items": [
			{
				"key": "test-key",
				"name": "test-name"
			}
		]
	}`

	t.Run("with valid flags calls API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", mockArgs...).
			Return([]byte(stubbedResponse), nil)
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects", "list",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--output", "json",
		}

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, stubbedResponse, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := projects.MockClient{}
		client.
			On("List", mockArgs...).
			Return([]byte(stubbedResponse), nil)
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects",
			"list",
			"--output", "json",
		}

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, stubbedResponse, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", mockArgs...).
			Return([]byte(`{}`), errors.NewError("an error"))
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects", "list",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
		}

		_, err := cmd.CallCmd(t, clients, args)

		require.EqualError(t, err, "an error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects", "list",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, `required flag(s) "access-token" not set`+errorHelp)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects", "list",
			"--access-token",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, `flag needs an argument: --access-token`)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects", "list",
			"--access-token", "testAccessToken",
			"--base-uri", "invalid",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})
}
