package flags_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd"
	"ldcli/internal/analytics"
	"ldcli/internal/resources"
)

func TestToggleOn(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{
			"key": "test-flag",
			"name": "test flag"
		}`),
	}
	args := []string{
		"flags", "toggle-on",
		"--access-token", "abcd1234",
		"--environment", "test-env",
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
	assert.Equal(t, `[{"op": "replace", "path": "/environments/test-env/on", "value": true}]`, string(mockClient.Input))
	assert.Equal(t, "Successfully updated test flag (test-flag)\n", string(output))
}
