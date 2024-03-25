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
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(cmd.ValidResponse), nil)

		output, err := cmd.CallCmd(t, &client, cmd.ArgsValidList())

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an unauthorized response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errors.NewError("You are not authorized to make this request"))

		_, err := cmd.CallCmd(t, &client, cmd.ArgsValidList())

		require.EqualError(t, err, "You are not authorized to make this request")
	})

	t.Run("with a forbidden response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errors.NewError("You do not have permission to make this request"))

		_, err := cmd.CallCmd(t, &client, cmd.ArgsValidList())

		require.EqualError(t, err, "You do not have permission to make this request")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		_, err := cmd.CallCmd(t, &projects.MockClient{}, cmd.ArgsListCommand())

		assert.EqualError(t, err, `required flag(s) "accessToken" not set`)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		_, err := cmd.CallCmd(t, &projects.MockClient{}, append(cmd.ArgsListCommand(), "--baseUri", "invalid"))

		assert.EqualError(t, err, "baseUri is invalid")
	})
}
