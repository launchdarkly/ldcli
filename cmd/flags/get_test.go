package flags_test

import (
	"ldcli/cmd"
	"ldcli/internal/analytics"
	"ldcli/internal/errors"
	"ldcli/internal/flags"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	errorHelp := ". See `ldcli flags get --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		"test-key",
		"test-proj-key",
		"test-env-key",
	}

	t.Run("with valid flags calls API", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.StubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			FlagsClient: &client,
		}
		args := []string{
			"flags", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--output", "json",
			"--flag", "test-key",
			"--project", "test-proj-key",
			"--environment", "test-env-key",
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, cmd.StubbedSuccessResponse, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := flags.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.StubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			FlagsClient: &client,
		}
		args := []string{
			"flags", "get",
			"--flag", "test-key",
			"--output", "json",
			"--project", "test-proj-key",
			"--environment", "test-env-key",
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, cmd.StubbedSuccessResponse, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(`{}`), errors.NewError(`{"message": "An error"}`))
		clients := cmd.APIClients{
			FlagsClient: &client,
		}
		args := []string{
			"flags", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--flag", "test-key",
			"--project", "test-proj-key",
			"--environment", "test-env-key",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			FlagsClient: &flags.MockClient{},
		}
		args := []string{
			"flags", "get",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "environment", "flag", "project" not set`+cmd.ExtraErrorHelp("flags", "get"))
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			FlagsClient: &flags.MockClient{},
		}
		args := []string{
			"flags", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "invalid",
			"--flag", "test-key",
			"--project", "test-proj-key",
			"--environment", "test-env-key",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})

	t.Run("will track analytics for CLI Command Run event", func(t *testing.T) {
		tracker := analytics.MockedTracker(
			"flags",
			"get",
			[]string{
				"access-token",
				"base-uri",
				"environment",
				"flag",
				"output",
				"project",
			}, analytics.SUCCESS)

		client := flags.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.StubbedSuccessResponse), nil)
		clients := cmd.APIClients{
			FlagsClient: &client,
		}

		args := []string{
			"flags", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--output", "json",
			"--flag", "test-key",
			"--project", "test-proj-key",
			"--environment", "test-env-key",
		}

		_, err := cmd.CallCmd(t, clients, tracker, args)
		require.NoError(t, err)
	})

	t.Run("will track analytics for api error", func(t *testing.T) {
		tracker := analytics.MockedTracker(
			"flags",
			"get",
			[]string{
				"access-token",
				"base-uri",
				"environment",
				"flag",
				"project",
			}, analytics.ERROR)
		client := flags.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(`{}`), errors.NewError(`{"message": "An error"}`))
		clients := cmd.APIClients{
			FlagsClient: &client,
		}

		args := []string{
			"flags", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--flag", "test-key",
			"--project", "test-proj-key",
			"--environment", "test-env-key",
		}

		_, err := cmd.CallCmd(t, clients, tracker, args)
		require.EqualError(t, err, "An error")
	})
}
