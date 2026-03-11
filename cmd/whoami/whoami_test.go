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
	t.Run("with configured token prints caller identity", func(t *testing.T) {
		mockClient := &resources.MockClient{
			Response: []byte(`{"tokenName": "my-token", "authKind": "token", "memberId": "abc123"}`),
		}

		t.Setenv("LD_ACCESS_TOKEN", "abcd1234")

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"whoami"},
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), "my-token")
	})

	t.Run("without configured token returns helpful error", func(t *testing.T) {
		_, err := cmd.CallCmd(
			t,
			cmd.APIClients{},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"whoami"},
		)

		require.ErrorContains(t, err, "no access token configured")
	})

	t.Run("with --output json returns raw JSON", func(t *testing.T) {
		mockClient := &resources.MockClient{
			Response: []byte(`{"tokenName": "my-token", "authKind": "token", "memberId": "abc123"}`),
		}

		t.Setenv("LD_ACCESS_TOKEN", "abcd1234")

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"whoami", "--output", "json"},
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), `"tokenName": "my-token"`)
	})
}
