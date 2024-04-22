package flags_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/analytics"
	"ldcli/internal/errors"
	"ldcli/internal/flags"
)

func TestCreate(t *testing.T) {
	errorHelp := ". See `ldcli flags create --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		"test-name",
		"test-key",
		"test-proj-key",
	}

	t.Run("with valid flags calls API", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			FlagsClient: &client,
		}
		args := []string{
			"flags", "create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--project", "test-proj-key",
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := flags.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			FlagsClient: &client,
		}
		args := []string{
			"flags", "create",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--project", "test-proj-key",
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		clients := cmd.APIClients{
			FlagsClient: &client,
		}
		args := []string{
			"flags", "create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--project", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			FlagsClient: &flags.MockClient{},
		}
		args := []string{
			"flags", "create",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "data", "project" not set`+errorHelp)
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			FlagsClient: &flags.MockClient{},
		}
		args := []string{
			"flags", "create",
			"-d",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: 'd' in -d`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			FlagsClient: &flags.MockClient{},
		}
		args := []string{
			"flags", "create",
			"--data",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: --data`)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			FlagsClient: &flags.MockClient{},
		}
		args := []string{
			"flags", "create",
			"--access-token", "testAccessToken",
			"--base-uri", "invalid",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--project", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})

	t.Run("will track analytics for CLI Command Run event", func(t *testing.T) {
		tracker := analytics.MockedTracker(
			"flags",
			"create",
			[]string{
				"access-token",
				"base-uri",
				"data",
				"project",
			})

		client := flags.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			FlagsClient: &client,
		}

		args := []string{
			"flags", "create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--project", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, clients, tracker, args)
		require.NoError(t, err)
	})
}
