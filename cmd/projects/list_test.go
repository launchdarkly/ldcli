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
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"projects", "list",
			"-t", "testAccessToken",
			"-u", "http://test.com",
		}

		output, err := cmd.CallCmd(t, nil, nil, &client, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", mockArgs...).
			Return([]byte(`{}`), errors.NewError("an error"))
		args := []string{
			"projects", "list",
			"-t", "testAccessToken",
			"-u", "http://test.com",
		}

		_, err := cmd.CallCmd(t, nil, nil, &client, args)

		require.EqualError(t, err, "an error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		args := []string{
			"projects", "list",
		}

		_, err := cmd.CallCmd(t, nil, nil, &projects.MockClient{}, args)

		assert.EqualError(t, err, `required flag(s) "accessToken" not set`+errorHelp)
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		args := []string{
			"projects", "list",
			"-t",
		}

		_, err := cmd.CallCmd(t, nil, &projects.MockClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: 't' in -t`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		args := []string{
			"projects", "list",
			"--accessToken",
		}

		_, err := cmd.CallCmd(t, nil, &projects.MockClient{}, args)

		assert.EqualError(t, err, `flag needs an argument: --accessToken`)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		args := []string{
			"projects", "list",
			"-t", "testAccessToken",
			"-u", "invalid",
		}

		_, err := cmd.CallCmd(t, nil, nil, &projects.MockClient{}, args)

		assert.EqualError(t, err, "baseUri is invalid"+errorHelp)
	})
}
