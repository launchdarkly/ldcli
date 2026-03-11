package whoami_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestWhoAmI(t *testing.T) {
	t.Run("with valid token prints caller identity", func(t *testing.T) {
		mockClient := &resources.MockClient{
			Response: []byte(`{"tokenName": "my-token", "authKind": "token", "memberId": "abc123"}`),
		}
		args := []string{
			"whoami",
			"--access-token", "abcd1234",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), "my-token")
	})

	t.Run("with --output json returns raw JSON", func(t *testing.T) {
		mockClient := &resources.MockClient{
			Response: []byte(`{"tokenName": "my-token", "authKind": "token", "memberId": "abc123"}`),
		}
		args := []string{
			"whoami",
			"--access-token", "abcd1234",
			"--output", "json",
		}

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			args,
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"tokenName": "my-token"`)
	})
}
