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
			"name": "test flag",
			"kind": "boolean",
			"archived": true
		}`),
	}

	t.Run("succeeds with plaintext output", func(t *testing.T) {
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
		assert.Contains(t, string(output), "Successfully updated\n\nKey:")
		assert.Contains(t, string(output), "test-flag")
		assert.Contains(t, string(output), "Name:")
		assert.Contains(t, string(output), "test flag")
		assert.NotContains(t, string(output), "* ")
	})

	t.Run("succeeds with JSON output", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
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
		assert.JSONEq(t, `{"key":"test-flag","name":"test flag","kind":"boolean","archived":true}`, string(output))
	})

	t.Run("succeeds with --json shorthand", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--json",
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
		assert.JSONEq(t, `{"key":"test-flag","name":"test flag","kind":"boolean","archived":true}`, string(output))
	})

	t.Run("filters JSON output with --fields", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
			"--fields", "key,name",
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
		assert.JSONEq(t, `{"key":"test-flag","name":"test flag"}`, string(output))
	})

	t.Run("filters JSON output with --json and --fields", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--json",
			"--fields", "key,name",
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
		assert.JSONEq(t, `{"key":"test-flag","name":"test flag"}`, string(output))
	})

	t.Run("ignores --fields with plaintext output", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--fields", "key",
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
		assert.Contains(t, string(output), "Successfully updated")
		assert.Contains(t, string(output), "Key:")
		assert.Contains(t, string(output), "test-flag")
	})

	t.Run("succeeds with markdown output", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "markdown",
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
		assert.Contains(t, string(output), "Successfully updated")
		assert.Contains(t, string(output), "## test-flag")
		assert.Contains(t, string(output), "- **Kind:** boolean")
	})

	t.Run("passes dryRun query param when --dry-run is set", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
			"--dry-run",
		}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient: mockClient,
			},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Equal(t, "true", mockClient.Query.Get("dryRun"))
	})

	t.Run("does not pass dryRun query param by default", func(t *testing.T) {
		args := []string{
			"flags", "archive",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{
				ResourcesClient: mockClient,
			},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Empty(t, mockClient.Query.Get("dryRun"))
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
		assert.Contains(t, err.Error(), `required flag(s) "project" not set`)
	})
}
