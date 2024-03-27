package flags_test

import (
	"ldcli/cmd"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/errors"
	"ldcli/internal/flags"
)

func TestUpdate(t *testing.T) {
	errorHelp := ". See `ldcli flags update --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		"test-proj-key",
		"test-key",
		[]ldapi.PatchOperation{
			{
				Op:    "replace",
				Path:  "/name",
				Value: "new-name",
			},
		},
	}
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Update", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"flags", "update",
			"-t", "testAccessToken",
			"-u", "http://test.com",
			"-d", `[{"op": "replace", "path": "/name", "value": "new-name"}]`,
			"--key", "test-key",
			"--projKey", "test-proj-key",
		}

		output, err := cmd.CallCmd(t, &client, nil, nil, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := flags.MockClient{}
		client.
			On("Update", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		args := []string{
			"flags", "update",
			"-t", "testAccessToken",
			"-u", "http://test.com",
			"-d", `[{"op": "replace", "path": "/name", "value": "new-name"}]`,
			"--key", "test-key",
			"--projKey", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, &client, nil, nil, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		args := []string{
			"flags", "update",
		}

		_, err := cmd.CallCmd(t, &flags.MockClient{}, nil, nil, args)

		assert.EqualError(t, err, `required flag(s) "accessToken", "data", "key", "projKey" not set`+errorHelp)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		args := []string{
			"flags", "update",
			"-t", "testAccessToken",
			"-u", "invalid",
			"-d", `{"key": "test-key", "name": "test-name"}`,
			"--projKey", "test-proj-key",
		}

		_, err := cmd.CallCmd(t, &flags.MockClient{}, nil, nil, args)

		assert.EqualError(t, err, "baseUri is invalid"+errorHelp)
	})
}
