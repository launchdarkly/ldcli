package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestCreate(t *testing.T) {
	t.Run("with valid flags prints version", func(t *testing.T) {
		args := []string{
			"--version",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `ldcli version test`)
	})
}

func TestJSONFlag(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{"key": "test-key", "name": "test-name"}`),
	}

	t.Run("--json returns raw JSON output", func(t *testing.T) {
		args := []string{
			"flags", "toggle-on",
			"--access-token", "abcd1234",
			"--environment", "test-env",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--json",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"key": "test-key"`)
		assert.NotContains(t, string(output), "Successfully updated")
	})
}
