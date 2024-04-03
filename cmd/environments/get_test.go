package environments_test

import (
	"ldcli/cmd"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/environments"
	"ldcli/internal/errors"
)

func TestGet(t *testing.T) {
	errorHelp := ". See `ldcli environments get --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"https://app.launchdarkly.com",
		"test-env",
		"test-proj",
	}
	t.Run("with valid environments calls projects API", func(t *testing.T) {
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		output, err := cmd.CallCmd(t, &client, nil, nil, nil, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, &client, nil, nil, nil, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required environments is an error", func(t *testing.T) {
		args := []string{
			"environments", "get",
		}

		_, err := cmd.CallCmd(t, &environments.MockClient{}, nil, nil, nil, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "environment", "project" not set`+errorHelp)
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		args := []string{
			"environments", "get",
			"-e",
		}

		_, err := cmd.CallCmd(t, &environments.MockClient{}, nil, nil, nil, args)

		assert.EqualError(t, err, `flag needs an argument: 'e' in -e`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		args := []string{
			"environments", "get",
			"--environment",
		}

		_, err := cmd.CallCmd(t, &environments.MockClient{}, nil, nil, nil, args)

		assert.EqualError(t, err, `flag needs an argument: --environment`)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "invalid",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, &environments.MockClient{}, nil, nil, nil, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})
}
