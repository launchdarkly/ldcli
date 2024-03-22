package projects_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ld-cli/cmd"
	errs "ld-cli/internal/errors"
	"ld-cli/internal/projects"
)

type argOptionFn func() []string

var (
	validResponse = `{"valid": true}`
)

func WithValidArgs() argOptionFn {
	return func() []string {
		return []string{
			"projects",
			"list",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
		}
	}
}

func WithNoArgs() argOptionFn {
	return func() []string {
		return []string{
			"projects",
			"list",
		}
	}
}

func callCmd(t *testing.T, client *projects.MockClient, options ...argOptionFn) ([]byte, error) {
	rootCmd, err := cmd.NewRootCmd(client)
	require.NoError(t, err)
	b := bytes.NewBufferString("")
	rootCmd.Cmd.SetOut(b)

	args := make([]string, 0)
	switch len(options) {
	case 0:
		args = WithValidArgs()()
	case 1:
		for _, o := range options {
			args = o()
		}
	default:
		return nil, errors.New("only one slice of arguments is valid")
	}
	rootCmd.Cmd.SetArgs(args)

	err = rootCmd.Cmd.Execute()
	if err != nil {
		return nil, err
	}

	out, err := io.ReadAll(b)
	require.NoError(t, err)

	return out, nil
}

func TestList2(t *testing.T) {
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(validResponse), nil)

		output, err := callCmd(t, &client)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an unauthorized response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errs.ErrUnauthorized)

		_, err := callCmd(t, &client)

		require.EqualError(t, err, "You are not authorized to make this request")
	})

	t.Run("with a forbidden response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errs.ErrForbidden)

		_, err := callCmd(t, &client)

		require.EqualError(t, err, "You do not have permission to make this request")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		_, err := callCmd(t, &projects.MockClient{}, WithNoArgs())

		assert.EqualError(t, err, `required flag(s) "accessToken", "baseUri" not set`)
	})
}

func TestList(t *testing.T) {
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{"valid": true}`), nil)
		rootCmd, err := cmd.NewRootCmd(&client)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"list",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
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
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errs.ErrUnauthorized)
		rootCmd, err := cmd.NewRootCmd(&client)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"list",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
		})
		err = rootCmd.Cmd.Execute()

		require.EqualError(t, err, "You are not authorized to make this request")
	})

	t.Run("with a forbidden response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("List", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errs.ErrForbidden)
		rootCmd, err := cmd.NewRootCmd(&client)
		require.NoError(t, err)
		b := bytes.NewBufferString("")
		rootCmd.Cmd.SetOut(b)
		rootCmd.Cmd.SetArgs([]string{
			"projects",
			"list",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
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
			"list",
		})
		err = rootCmd.Cmd.Execute()

		assert.EqualError(t, err, `required flag(s) "accessToken", "baseUri" not set`)
	})
}
