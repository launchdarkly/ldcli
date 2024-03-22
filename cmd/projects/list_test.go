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

var (
	validResponse = `{"valid": true}`
)

func ArgsValidCreate() []string {
	args := append(ArgsCreateCommand(), ArgsAccess()...)
	args = append(args, ArgsData()...)

	return args
}

func ArgsValidList() []string {
	return append(ArgsListCommand(), ArgsAccess()...)
}

func ArgsData() []string {
	return []string{
		"-d",
		`{"key": "test-key", "name": "test-name"}`,
	}
}

func ArgsAccess() []string {
	return []string{
		"-t",
		"testAccessToken",
		"-u",
		"http://test.com",
	}
}

func ArgsCreateCommand() []string {
	return []string{
		"projects",
		"create",
	}
}

func ArgsListCommand() []string {
	return []string{
		"projects",
		"list",
	}
}

func callCmd(t *testing.T, client *projects.MockClient, args []string) ([]byte, error) {
	rootCmd, err := cmd.NewRootCmd(client)
	require.NoError(t, err)
	b := bytes.NewBufferString("")
	rootCmd.Cmd.SetOut(b)
	rootCmd.Cmd.SetArgs(args)

	err = rootCmd.Cmd.Execute()
	if err != nil {
		return nil, err
	}

	out, err := io.ReadAll(b)
	require.NoError(t, err)

	return out, nil
}

func TestList(t *testing.T) {
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(validResponse), nil)

		output, err := callCmd(t, &client, ArgsValidList())

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an unauthorized response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errors.ErrUnauthorized)

		_, err := callCmd(t, &client, ArgsValidList())

		require.EqualError(t, err, "You are not authorized to make this request")
	})

	t.Run("with a forbidden response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errors.ErrForbidden)

		_, err := callCmd(t, &client, ArgsValidList())

		require.EqualError(t, err, "You do not have permission to make this request")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		_, err := callCmd(t, &projects.MockClient{}, ArgsListCommand())

		assert.EqualError(t, err, `required flag(s) "accessToken", "baseUri" not set`)
	})
}
