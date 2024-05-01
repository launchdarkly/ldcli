package environments_test

import (
	"ldcli/cmd"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/analytics"
	"ldcli/internal/environments"
	"ldcli/internal/errors"
)

func TestGet(t *testing.T) {
	errorHelp := ". See `ldcli environments get --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		"test-env",
		"test-proj",
	}
	stubbedResponse := `{"key": "test-key", "name": "test-name"}`

	t.Run("with valid flags calls API", func(t *testing.T) {
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(stubbedResponse), nil)
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--output", "json",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, stubbedResponse, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(stubbedResponse), nil)
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}
		args := []string{
			"environments", "get",
			"--output", "json",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, stubbedResponse, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(`{}`), errors.NewError(`{"message": "An error"}`))
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required environments is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "environment", "project" not set`+cmd.ExtraErrorHelp("environments", "get"))
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"-e",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: 'e' in -e`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"--environment",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: --environment`)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "invalid",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})

	t.Run("with invalid output is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--output", "invalid",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, "output is invalid"+errorHelp)
	})

	t.Run("will track analytics for CLI Command Run event", func(t *testing.T) {
		tracker := analytics.MockedTracker(
			"environments",
			"get",
			[]string{
				"access-token",
				"base-uri",
				"environment",
				"project",
			}, analytics.SUCCESS)
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.StubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}

		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, clients, tracker, args)

		require.NoError(t, err)
	})
}
