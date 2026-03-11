package whoami_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

// sequentialMockClient returns responses in order, one per call.
type sequentialMockClient struct {
	responses [][]byte
	callIndex int
}

var _ resources.Client = &sequentialMockClient{}

func (c *sequentialMockClient) MakeRequest(_, _, _ string, _ string, _ url.Values, _ []byte, _ bool) ([]byte, error) {
	if c.callIndex >= len(c.responses) {
		return nil, nil
	}
	res := c.responses[c.callIndex]
	c.callIndex++
	return res, nil
}

func (c *sequentialMockClient) MakeUnauthenticatedRequest(_ string, _ string, _ []byte) ([]byte, error) {
	return nil, nil
}

func TestWhoAmI(t *testing.T) {
	t.Run("shows member name, email, role, and token", func(t *testing.T) {
		mockClient := &sequentialMockClient{
			responses: [][]byte{
				[]byte(`{"memberId": "abc123", "tokenName": "my-token", "tokenKind": "personal", "accountId": "acct1"}`),
				[]byte(`{"_id": "abc123", "email": "ariel@acme.com", "firstName": "Ariel", "lastName": "Flores", "role": "admin"}`),
			},
		}

		t.Setenv("LD_ACCESS_TOKEN", "abcd1234")

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"whoami"},
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), "Ariel Flores <ariel@acme.com>")
		assert.Contains(t, string(output), "Role:    admin")
		assert.Contains(t, string(output), "Token:   my-token (personal)")
	})

	t.Run("without member ID shows token info only", func(t *testing.T) {
		mockClient := &resources.MockClient{
			Response: []byte(`{"tokenName": "sdk-key", "tokenKind": "server", "accountId": "acct1"}`),
		}

		t.Setenv("LD_ACCESS_TOKEN", "abcd1234")

		output, err := cmd.CallCmd(
			t,
			cmd.APIClients{ResourcesClient: mockClient},
			analytics.NoopClientFn{}.Tracker(),
			[]string{"whoami"},
		)

		require.NoError(t, err)
		assert.Contains(t, string(output), "Token:   sdk-key (server)")
		assert.NotContains(t, string(output), "Role:")
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

	t.Run("with --output json returns raw caller-identity JSON", func(t *testing.T) {
		mockClient := &resources.MockClient{
			Response: []byte(`{"tokenName": "my-token", "memberId": "abc123"}`),
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
