package flags_test

import (
	"ldcli/cmd"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"flags", "create",
			"-t", "testAccessToken",
			"-u", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--projKey", "test-proj-key",
		}

		output, err := cmd.CallCmd(t, &client, nil, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		args := []string{
			"flags", "create",
			"-t", "testAccessToken",
			"-u", "http://test.com",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--projKey", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, &client, nil, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		args := []string{
			"flags", "create",
		}

		_, err := cmd.CallCmd(t, &flags.MockClient{}, nil, args)

		assert.EqualError(t, err, `required flag(s) "accessToken", "data", "projKey" not set`+errorHelp)
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		args := []string{
			"flags", "create",
			"-d",
		}

		_, err := cmd.CallCmd(t, &flags.MockClient{}, nil, args)

		assert.EqualError(t, err, `flag needs an argument: 'd' in -d`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		args := []string{
			"flags", "create",
			"--data",
		}

		_, err := cmd.CallCmd(t, &flags.MockClient{}, nil, args)

		assert.EqualError(t, err, `flag needs an argument: --data`)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		args := []string{
			"flags", "create",
			"-t", "testAccessToken",
			"-u", "invalid",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--projKey", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, &flags.MockClient{}, nil, args)

		assert.EqualError(t, err, "baseUri is invalid"+errorHelp)
	})
}
