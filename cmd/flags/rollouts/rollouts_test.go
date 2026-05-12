package rollouts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

func TestBetaBanner(t *testing.T) {
	const bannerSubstr = "rollouts-beta is unstable"

	t.Run("prints beta banner on TTY when --output is not json", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "1")
		mockClient := &rollouts.MockClient{}
		mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
			Return(&rollouts.RolloutList{Items: []rollouts.Rollout{
				{
					ID:             "r1",
					Kind:           "guarded",
					EnvironmentKey: "prod",
					CreatedAt:      time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC),
					Status:         rollouts.StatusBlock{Status: "active", Kind: "active", Label: "Rolling out"},
				},
			}}, nil)
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "plaintext",
		}
		_, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		assert.Contains(t, string(stderr), bannerSubstr, "expected banner on stderr; stderr=%q", string(stderr))
	})

	t.Run("suppresses beta banner with --output json", func(t *testing.T) {
		t.Setenv("FORCE_TTY", "1")
		mockClient := &rollouts.MockClient{}
		mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
			Return(&rollouts.RolloutList{Items: []rollouts.Rollout{}}, nil)
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "json",
		}
		_, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		assert.NotContains(t, string(stderr), bannerSubstr, "expected no banner with --output json; stderr=%q", string(stderr))
	})

	t.Run("suppresses beta banner when stderr is not a TTY", func(t *testing.T) {
		// Do NOT set FORCE_TTY. In `go test`, os.Stderr is typically piped, not a TTY, so
		// term.IsTerminal returns false and the banner should be suppressed.
		// Make sure no inherited FORCE_TTY leaks in:
		t.Setenv("FORCE_TTY", "")
		t.Setenv("LD_FORCE_TTY", "")
		mockClient := &rollouts.MockClient{}
		mockClient.On("List", "abcd1234", mock.Anything, "test-proj", "test-flag", mock.Anything).
			Return(&rollouts.RolloutList{Items: []rollouts.Rollout{}}, nil)
		args := []string{
			"flags", "rollouts-beta", "list",
			"--access-token", "abcd1234",
			"--flag", "test-flag",
			"--project", "test-proj",
			"--output", "plaintext",
		}
		_, stderr, err := cmd.CallCmdWithStderr(t, cmd.APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)
		require.NoError(t, err)
		// Only assert banner suppression — if os.Stderr were ever a TTY in the test runner
		// this would need rework, but in standard go test (and CI) stderr is piped.
		s := strings.ToLower(string(stderr))
		assert.NotContains(t, s, strings.ToLower(bannerSubstr),
			"expected no banner when stderr is not a TTY (FORCE_TTY unset); stderr=%q", string(stderr))
	})
}
