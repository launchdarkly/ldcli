package projects_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ld-cli/cmd"
	"ld-cli/internal/errors"
	"ld-cli/internal/projects"
)

func TestList(t *testing.T) {
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := MockClient{}
		client.
			On("List2", "testAccessToken", "http://test.com").
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
		client := MockClient{}
		client.
			On("List2", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errors.ErrUnauthorized)
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
		client := MockClient{}
		client.
			On("List2", "testAccessToken", "http://test.com").
			Return([]byte(`{}`), errors.ErrForbidden)
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
		rootCmd, err := cmd.NewRootCmd(&MockClient{})
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

type MockClient struct {
	mock.Mock

	HasForbiddenErr    bool
	HasUnauthorizedErr bool

	AccessToken string
	BaseURI     string
}

var _ projects.Client2 = &MockClient{}

func (c *MockClient) Create2(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key string,
) ([]byte, error) {
	args := c.Called(accessToken, baseURI, name, key)

	return args.Get(0).([]byte), args.Error(1)
}

func (c *MockClient) List2(
	ctx context.Context,
	accessToken,
	baseURI string,
) ([]byte, error) {
	if c.HasForbiddenErr {
		return nil, errors.ErrForbidden
	}
	if c.HasUnauthorizedErr {
		return nil, errors.ErrUnauthorized
	}

	args := c.Called(accessToken, baseURI)

	return args.Get(0).([]byte), args.Error(1)
}