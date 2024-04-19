package members_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/analytics"
	"ldcli/internal/errors"
	"ldcli/internal/members"
)

func TestCreate(t *testing.T) {
	errorHelp := ". See `ldcli members create --help` for supported flags and usage."
	role := "writer"
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		[]members.MemberInput{{Email: "testemail@test.com", Role: role}},
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
			"create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d",
			`[{"email": "testemail@test.com", "role": "writer"}]`,
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

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
			"create",
			"-d",
			`[{"email": "testemail@test.com", "role": "writer"}]`,
		}

		output, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

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
			"create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d",
			`[{"email": "testemail@test.com", "role": "writer"}]`,
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required flags is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			MembersClient: &members.MockClient{},
		}
		args := []string{
			"members",
			"create",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "data" not set`+errorHelp)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			MembersClient: &members.MockClient{},
		}
		args := []string{
			"members",
			"create",
			"--base-uri", "invalid",
		}

		_, err := cmd.CallCmd(t, clients, &analytics.NoopClient{}, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})

	t.Run("will track analytics for 'CLI Command Run' event", func(t *testing.T) {
		id := "test-id"
		mockedTrackingArgs := []interface{}{
			"testAccessToken",
			"http://test.com",
			"CLI Command Run",
			map[string]interface{}{
				"name":    "members",
				"action":  "create",
				"baseURI": "http://test.com",
				"id":      id,
				"flags":   []string{"access-token", "base-uri", "data"},
			},
		}
		tracker := analytics.MockTracker{ID: id}
		tracker.On("SendEvent", mockedTrackingArgs...)

		client := members.MockClient{}
		client.
			On("Create", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			MembersClient: &client,
		}
		args := []string{
			"members",
			"create",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"-d",
			`[{"email": "testemail@test.com", "role": "writer"}]`,
		}

		_, err := cmd.CallCmd(t, clients, &tracker, args)
		tracker.AssertCalled(t, "SendEvent", mockedTrackingArgs...)
		require.NoError(t, err)
	})
}
