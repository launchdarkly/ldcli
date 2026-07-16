package setup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
	"github.com/launchdarkly/ldcli/internal/setup"
)

func TestSetup_NoAuth_ReturnsLoginGuidance(t *testing.T) {
	// No --access-token and no LD_ACCESS_TOKEN: the wizard must bail before the
	// TUI with clear guidance rather than dumping a raw 401.
	args := []string{"setup"}
	_, err := cmd.CallCmd(
		t,
		cmd.APIClients{ResourcesClient: &resources.MockClient{}},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ldcli login")
}

func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "index.js")

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
	filePath := filepath.Join(tmpDir, "index.js")

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

func TestInitUnsupportedSDKPlaintext(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/main.rs"

	args := []string{
		"setup", "init",
		"--access-token", "test-token",
		"--sdk-id", "rust-server-sdk",
		"--file", filePath,
		"--sdk-key", "test-sdk-key",
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
	assert.Contains(t, string(output), "No initialization template available for rust-server-sdk")
	assert.Contains(t, string(output), "setup guide at:")
	assert.NotContains(t, string(output), "Injected")
}

func TestInitUnsupportedSDKJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/main.rs"

	args := []string{
		"setup", "init",
		"--access-token", "test-token",
		"--sdk-id", "rust-server-sdk",
		"--file", filePath,
		"--sdk-key", "test-sdk-key",
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
	assert.Contains(t, string(output), `"success":false`)
	assert.Contains(t, string(output), `"docs_url"`)
}

func TestDetect_UnknownProject_ReturnsError(t *testing.T) {
	emptyDir := t.TempDir()
	args := []string{
		"setup", "detect",
		"--access-token", "test-token",
		"--path", emptyDir,
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
	assert.Contains(t, err.Error(), "could not detect")
}

func TestDetect_GoProject_ReturnsResult(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.21\n"), 0600)
	require.NoError(t, err)

	args := []string{
		"setup", "detect",
		"--access-token", "test-token",
		"--path", dir,
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
	assert.Contains(t, string(output), "go-server-sdk")
}

func TestDetect_JSON(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.21\n"), 0600)
	require.NoError(t, err)

	args := []string{
		"setup", "detect",
		"--access-token", "test-token",
		"--path", dir,
		"--output", "json",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{ResourcesClient: &resources.MockClient{}},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), `"sdk_id":"go-server-sdk"`)
}

// mockInstaller is a simple Installer that returns a canned result, used to exercise
// runInstall output paths without executing real package manager commands.
type mockInstaller struct {
	result *setup.InstallResult
}

func (m mockInstaller) Install(_ string, detection *setup.DetectResult) (*setup.InstallResult, error) {
	if m.result != nil {
		return m.result, nil
	}
	return &setup.InstallResult{
		SDKID:   detection.SDKID,
		Package: "@launchdarkly/node-server-sdk",
		Command: "npm install @launchdarkly/node-server-sdk",
		Success: true,
	}, nil
}

func TestInstall_Plaintext(t *testing.T) {
	args := []string{
		"setup", "install",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
			Installer:       mockInstaller{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), "node-server")
	assert.Contains(t, string(output), "@launchdarkly/node-server-sdk")
}

func TestInstall_Plaintext_WithVersion(t *testing.T) {
	args := []string{
		"setup", "install",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
			Installer: mockInstaller{result: &setup.InstallResult{
				SDKID:   "node-server",
				Package: "@launchdarkly/node-server-sdk",
				Version: "9.7.0",
				Command: "npm install @launchdarkly/node-server-sdk",
				Success: true,
			}},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), "@launchdarkly/node-server-sdk@9.7.0")
}

func TestInstall_DryRun(t *testing.T) {
	args := []string{
		"setup", "install",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
		"--dry-run",
	}
	// No Installer provided: dry-run must not invoke it or shell out.
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), "npm install @launchdarkly/node-server-sdk")
	assert.Contains(t, string(output), "Dry run")
}

func TestInstall_JSON(t *testing.T) {
	args := []string{
		"setup", "install",
		"--access-token", "test-token",
		"--sdk-id", "node-server",
		"--output", "json",
	}
	output, err := cmd.CallCmd(
		t,
		cmd.APIClients{
			ResourcesClient: &resources.MockClient{},
			Installer:       mockInstaller{},
		},
		analytics.NoopClientFn{}.Tracker(),
		args,
	)

	require.NoError(t, err)
	assert.Contains(t, string(output), `"success":true`)
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
			Installer:       setup.StubInstaller{},
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
