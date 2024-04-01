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
			"--access-token",
			"testAccessToken",
			"--base-uri",
			"http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		output, err := cmd.CallCmd(t, nil, nil, &client, nil, args)

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
			"--access-token",
			"testAccessToken",
			"--base-uri",
			"http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		_, err := cmd.CallCmd(t, nil, nil, &client, nil, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		args := []string{
			"members",
			"invite",
		}

		_, err := cmd.CallCmd(t, nil, nil, &members.MockClient{}, nil, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "emails" not set`+errorHelp)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		args := []string{
			"members",
			"invite",
			"--base-uri", "invalid",
		}

		_, err := cmd.CallCmd(t, nil, nil, &members.MockClient{}, nil, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})
}

func TestInviteWithOptionalRole(t *testing.T) {
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		[]string{"testemail1@test.com", "testemail2@test.com"},
		"writer",
	}
	t.Run("with valid optional long form flag calls members API", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"members",
			"invite",
			"--access-token",
			"testAccessToken",
			"--base-uri",
			"http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
			"--role",
			"writer",
		}

		output, err := cmd.CallCmd(t, nil, nil, &client, nil, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with valid optional short form flag calls members API", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		args := []string{
			"members",
			"invite",
			"--access-token",
			"testAccessToken",
			"--base-uri",
			"http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
			"-r",
			"writer",
		}

		output, err := cmd.CallCmd(t, nil, nil, &client, nil, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})
}
