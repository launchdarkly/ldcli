package projects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ld-cli/cmd"
	"ld-cli/internal/errors"
	"ld-cli/internal/projects"
)

func TestCreate(t *testing.T) {
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(cmd.ValidResponse), nil)

		output, err := cmd.CallCmd(t, &client, cmd.ArgsValidCreate())

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an unauthorized response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(`{}`), errors.ErrUnauthorized)

		_, err := cmd.CallCmd(t, &client, cmd.ArgsValidCreate())

		require.EqualError(t, err, "You are not authorized to make this request")
	})

	t.Run("with a forbidden response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(`{}`), errors.ErrForbidden)

		_, err := cmd.CallCmd(t, &client, cmd.ArgsValidCreate())

		require.EqualError(t, err, "You do not have permission to make this request")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		_, err := cmd.CallCmd(t, &projects.MockClient{}, cmd.ArgsCreateCommand())

		assert.EqualError(t, err, `required flag(s) "accessToken", "data" not set`)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		_, err := cmd.CallCmd(t, &projects.MockClient{}, append(cmd.ArgsCreateCommand(), "--baseUri", "invalid"))

		assert.EqualError(t, err, "baseUri is invalid")
	})
}
