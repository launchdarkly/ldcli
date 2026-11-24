package resources_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

// A GET that returns no body, such as the default release plan of an
// environment that has none, must not print the last path parameter as a key.
func TestEmptyResponse(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "GET with JSON output prints an empty object",
			args:     []string{"projects", "get", "--project", "my-project", "--output", "json"},
			expected: "{}\n",
		},
		{
			name:     "GET with plaintext output says no resource was returned",
			args:     []string{"projects", "get", "--project", "my-project", "--output", "plaintext"},
			expected: "No projects returned\n",
		},
		{
			name:     "GET on a beta resource drops the suffix from the message",
			args:     []string{"flags-beta", "list-dependent", "--project", "my-project", "--flag", "my-flag", "--output", "plaintext"},
			expected: "No flags returned\n",
		},
		{
			name:     "DELETE still prints the key of the deleted resource",
			args:     []string{"projects", "delete", "--project", "my-project", "--output", "json"},
			expected: `{"key": "my-project"}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &resources.MockClient{Response: []byte("")}
			args := append(tt.args, "--access-token", "abcd1234")

			output, err := cmd.CallCmd(
				t,
				cmd.APIClients{ResourcesClient: client},
				analytics.NoopClientFn{}.Tracker(),
				args,
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(output))
		})
	}
}
