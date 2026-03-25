package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
)

func TestOnboard(t *testing.T) {
	t.Run("with JSON output returns valid JSON plan", func(t *testing.T) {
		args := []string{
			"onboard",
			"--access-token", "test-token",
			"--project", "default",
			"--environment", "test",
			"--output", "json",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"steps"`)
		assert.Contains(t, string(output), `"sdk_recipes"`)
		assert.Contains(t, string(output), `"recovery_options"`)
		assert.Contains(t, string(output), `"detect"`)
	})

	t.Run("with plaintext output returns readable summary", func(t *testing.T) {
		args := []string{
			"onboard",
			"--access-token", "test-token",
			"--project", "default",
			"--environment", "test",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), "LaunchDarkly AI-Agent Onboarding Plan")
		assert.Contains(t, string(output), "Detect Repository Stack")
		assert.Contains(t, string(output), "--json")
	})

	t.Run("with --json flag returns JSON output", func(t *testing.T) {
		args := []string{
			"onboard",
			"--access-token", "test-token",
			"--project", "default",
			"--environment", "test",
			"--json",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"steps"`)
	})
}
