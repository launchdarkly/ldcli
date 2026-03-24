package sdk_active_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestGetSdkActive(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{
			"sdkActive": true,
			"lastSeenAt": 1718000000000
		}`),
	}
	args := []string{
		"environments", "get-sdk-active",
		"--access-token", "abcd1234",
		"--project", "test-proj",
		"--environment", "test-env",
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
	assert.Equal(t, "SDK active: true\n", string(output))
}

func TestGetSdkActiveJSON(t *testing.T) {
	mockClient := &resources.MockClient{
		Response: []byte(`{
			"sdkActive": true,
			"lastSeenAt": 1718000000000
		}`),
	}
	args := []string{
		"environments", "get-sdk-active",
		"--access-token", "abcd1234",
		"--project", "test-proj",
		"--environment", "test-env",
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
	assert.Contains(t, string(output), `"sdkActive"`)
	assert.Contains(t, string(output), `"lastSeenAt"`)
}

func TestGetSdkActiveMissingRequiredFlags(t *testing.T) {
	mockClient := &resources.MockClient{}
	args := []string{
		"environments", "get-sdk-active",
		"--access-token", "abcd1234",
	}
	_, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: mockClient,
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}
