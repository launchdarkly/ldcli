package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/index.js"

	args := []string{
		"setup", "init",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
		"--file", filePath,
		"--sdk-key", "test-sdk-key",
		"--flag-key", "test-flag",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), "Injected node-server")
}

func TestInitJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/index.js"

	args := []string{
		"setup", "init",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
		"--file", filePath,
		"--sdk-key", "test-sdk-key",
		"--flag-key", "test-flag",
		"--output", "json",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), `"success":true`)
}

func TestDetectStubReturnsError(t *testing.T) {
	args := []string{
		"setup", "detect",
		"--access-token", "test-token",
	}
	_, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestInstallStubReturnsError(t *testing.T) {
	args := []string{
		"setup", "install",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
	}
	_, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestInstallMissingRequiredFlag(t *testing.T) {
	args := []string{
		"setup", "install",
		"--access-token", "test-token",
	}
	_, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag")
}

func TestInitMissingRequiredFlags(t *testing.T) {
	args := []string{
		"setup", "init",
		"--access-token", "test-token",
	}
	_, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag")
}
