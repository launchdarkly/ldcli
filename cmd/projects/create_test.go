package projects_test

import (
	"bytes"
	"io"
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
			On("Create2", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(`{"valid": true}`), nil)
		rootCmd, err := cmd.NewRootCmd(&client)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"create",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		})
		err = rootCmd.Cmd.Execute()

		require.NoError(t, err)
		out, err := io.ReadAll(b)
		require.NoError(t, err)

		assert.JSONEq(t, `{"valid": true}`, string(out))
	})

	t.Run("with an unauthorized response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create2", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(`{}`), errors.ErrUnauthorized)
		rootCmd, err := cmd.NewRootCmd(&client)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"create",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		})
		err = rootCmd.Cmd.Execute()

		require.EqualError(t, err, "You are not authorized to make this request")
	})

	t.Run("with a forbidden response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create2", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(`{}`), errors.ErrForbidden)
		rootCmd, err := cmd.NewRootCmd(&client)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"create",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		})
		err = rootCmd.Cmd.Execute()

		require.EqualError(t, err, "You do not have permission to make this request")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		rootCmd, err := cmd.NewRootCmd(&projects.MockClient{})
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"create",
		})
		err = rootCmd.Cmd.Execute()

		assert.EqualError(t, err, `required flag(s) "accessToken", "baseUri", "data" not set`)
	})
}
