package projects_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/errors"
	"ldcli/internal/projects"
)

func TestCreate(t *testing.T) {
	t.Run("with valid flags calls projects API", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"projects",
			"create",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		}

		output, err := cmd.CallCmd(t, nil, &client, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := projects.MockClient{}
		client.
			On("Create", "testAccessToken", "http://test.com", "test-name", "test-key").
			Return([]byte(`{}`), errors.NewError("An error"))
		args := []string{
			"projects",
			"create",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-d",
			`{"key": "test-key", "name": "test-name"}`,
		}

		_, err := cmd.CallCmd(t, nil, &client, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		args := []string{
			"projects",
			"create",
		}

		_, err := cmd.CallCmd(t, nil, &projects.MockClient{}, args)

		assert.EqualError(t, err, `required flag(s) "accessToken", "data" not set`)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		args := []string{
			"projects",
			"create",
			"--baseUri", "invalid",
		}

		_, err := cmd.CallCmd(t, nil, &projects.MockClient{}, args)

		assert.EqualError(t, err, "baseUri is invalid")
	})
}
