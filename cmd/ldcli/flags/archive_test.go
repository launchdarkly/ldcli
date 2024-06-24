package flags_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestArchive(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{
			"key": "test-flag",
			"name": "test flag"
		}`),
	}

	t.Run("succeeds with valid inputs", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
		}
		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient: mockClient,
			},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Equal(t, `[{"op": "replace", "path": "/archived", "value": true}]`, string(mockClient.Input))
		assert.Equal(t, "Successfully updated test flag (test-flag)\n", string(output))
	})
	t.Run("returns error with missing flags", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
		}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient: mockClient,
			},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		assert.Error(t, err)
		assert.Equal(t, "required flag(s) \"project\" not set. See `ldcli flags archive --help` for supported flags and usage.", err.Error())
	})
}
