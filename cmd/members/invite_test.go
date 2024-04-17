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
	readerRole := "reader"
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		[]members.MemberInput{
			{Email: "testemail1@test.com", Role: readerRole},
			{Email: "testemail2@test.com", Role: readerRole},
		},
	}

	t.Run("with valid flags calls members API", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			MembersClient: &client,
		}
		args := []string{
			"members",
			"invite",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := members.MockClient{}
		client.
			On("Update", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			MembersClient: &client,
		}
		args := []string{
			"members",
			"invite",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		clients := cmd.APIClients{
			MembersClient: &client,
		}
		args := []string{
			"members",
			"invite",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-e",
			`testemail1@test.com,testemail2@test.com`,
		}

		_, err := cmd.CallCmd(t, clients, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			MembersClient: &members.MockClient{},
		}
		args := []string{
			"members",
			"invite",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "emails" not set`+errorHelp)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			MembersClient: &members.MockClient{},
		}
		args := []string{
			"members",
			"invite",
			"--base-uri", "invalid",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})
}

func TestInviteWithOptionalRole(t *testing.T) {
	writerRole := "writer"
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		[]members.MemberInput{
			{Email: "testemail1@test.com", Role: writerRole},
			{Email: "testemail2@test.com", Role: writerRole},
		},
	}

	t.Run("with valid optional long form flag calls members API", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			MembersClient: &client,
		}
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

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with valid optional short form flag calls members API", func(t *testing.T) {
		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			MembersClient: &client,
		}
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

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})
}
