package members_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/errors"
	"ldcli/internal/members"
)

func TestInvite(t *testing.T) {
	errorHelp := ". See `ldcli members invite --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		[]string{"testemail1@test.com", "testemail2@test.com"},
		"reader",
	}
	t.Run("with valid flags calls members API", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"members",
			"invite",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		output, err := cmd.CallCmd(t, nil, &client, nil, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		args := []string{
			"members",
			"invite",
			"-t",
			"testAccessToken",
			"-u",
			"http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		_, err := cmd.CallCmd(t, nil, &client, nil, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		args := []string{
			"members",
			"invite",
		}

		_, err := cmd.CallCmd(t, nil, &members.MockClient{}, nil, args)

		assert.EqualError(t, err, `required flag(s) "accessToken", "emails" not set`+errorHelp)
	})

	t.Run("with invalid baseUri is an error", func(t *testing.T) {
		args := []string{
			"members",
			"invite",
			"--baseUri", "invalid",
		}

		_, err := cmd.CallCmd(t, nil, &members.MockClient{}, nil, args)

		assert.EqualError(t, err, "baseUri is invalid"+errorHelp)
	})
}
