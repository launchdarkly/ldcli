package projects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/analytics"
	"ldcli/internal/errors"
	"ldcli/internal/projects"
)

func TestCreate(t *testing.T) {
	errorHelp := ". See `ldcli projects create --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		"test-name",
		"test-key",
	}
	stubbedSuccessResponse := `{
		"key": "test-key",
		"name": "test-name"
	}`

	t.Run("with valid flags calls API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(stubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects",
			"create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--output", "json",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, stubbedSuccessResponse, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := projects.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(stubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects",
			"create",
			"--output", "json",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, stubbedSuccessResponse, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(`{}`), errors.NewError(`{"message": "An error"}`))
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects", "create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects",
			"create",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "data" not set`+cmd.ExtraErrorHelp("projects", "create"))
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects", "create",
			"-d",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: 'd' in -d`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects", "create",
			"--data",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: --data`)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			ProjectsClient: &projects.MockClient{},
		}
		args := []string{
			"projects",
			"create",
			"--base-uri", "invalid",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})

	t.Run("will track analytics for CLI Command Run event", func(t *testing.T) {
		tracker := analytics.MockedTracker(
			"projects",
			"create",
			[]string{
				"access-token",
				"base-uri",
				"data",
			}, analytics.SUCCESS)

		client := projects.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.StubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			ProjectsClient: &client,
		}
		args := []string{
			"projects", "create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
		}

		_, err := cmd.CallCmd(t, clients, tracker, args)
		require.NoError(t, err)
	})
}
