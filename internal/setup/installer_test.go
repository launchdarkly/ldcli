package setup

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallArgs_NodeSDKs(t *testing.T) {
	tests := []struct {
		sdkID   string
		pm      string
		wantCmd string
		wantPkg string
	}{
		{"react-client-sdk", "npm", "npm", "launchdarkly-react-client-sdk"},
		{"react-client-sdk", "yarn", "yarn", "launchdarkly-react-client-sdk"},
		{"react-client-sdk", "pnpm", "pnpm", "launchdarkly-react-client-sdk"},
		{"react-client-sdk", "bun", "bun", "launchdarkly-react-client-sdk"},
		{"react-client-sdk", "", "npm", "launchdarkly-react-client-sdk"},
		{"react-native", "npm", "npm", "launchdarkly-react-native-client-sdk"},
		{"react-native", "bun", "bun", "launchdarkly-react-native-client-sdk"},
		{"node-server", "npm", "npm", "@launchdarkly/node-server-sdk"},
		{"node-server", "yarn", "yarn", "@launchdarkly/node-server-sdk"},
		{"node-server", "pnpm", "pnpm", "@launchdarkly/node-server-sdk"},
		{"node-server", "bun", "bun", "@launchdarkly/node-server-sdk"},
		{"node-server", "", "npm", "@launchdarkly/node-server-sdk"},
		{"js-client-sdk", "npm", "npm", "@launchdarkly/js-client-sdk"},
		{"js-client-sdk", "bun", "bun", "@launchdarkly/js-client-sdk"},
	}

	for _, tt := range tests {
		t.Run(tt.sdkID+"/"+tt.pm, func(t *testing.T) {
			args, pkg := InstallArgs(tt.sdkID, tt.pm)
			require.NotEmpty(t, args)
			assert.Equal(t, tt.wantCmd, args[0])
			assert.Equal(t, tt.wantPkg, pkg)
			assert.Contains(t, args, pkg)
		})
	}
}

func TestInstallArgs_Python(t *testing.T) {
	args, pkg := InstallArgs("python-server-sdk", "")
	require.NotEmpty(t, args)
	assert.Equal(t, "pip", args[0])
	assert.Equal(t, "launchdarkly-server-sdk", pkg)

	args2, _ := InstallArgs("python-server-sdk", "pip3")
	require.NotEmpty(t, args2)
	assert.Equal(t, "pip3", args2[0])
}

func TestInstallArgs_Go(t *testing.T) {
	args, pkg := InstallArgs("go-server-sdk", "")
	require.NotEmpty(t, args)
	assert.Equal(t, "go", args[0])
	assert.Equal(t, "get", args[1])
	assert.Equal(t, "github.com/launchdarkly/go-server-sdk/v7", pkg)
}

func TestInstallArgs_Ruby(t *testing.T) {
	args, pkg := InstallArgs("ruby-server-sdk", "")
	require.NotEmpty(t, args)
	assert.Equal(t, "gem", args[0])
	assert.Equal(t, "launchdarkly-server-sdk", pkg)
}

func TestInstallArgs_Dotnet(t *testing.T) {
	args, pkg := InstallArgs("dotnet-server-sdk", "")
	require.NotEmpty(t, args)
	assert.Equal(t, "dotnet", args[0])
	assert.Equal(t, "LaunchDarkly.ServerSdk", pkg)
}

func TestInstallArgs_ManualSDKs(t *testing.T) {
	tests := []struct {
		sdkID   string
		wantPkg string
	}{
		{"java-server-sdk", "com.launchdarkly:launchdarkly-java-server-sdk"},
		{"android", "com.launchdarkly:launchdarkly-android-client-sdk"},
		{"android-client-sdk", "com.launchdarkly:launchdarkly-android-client-sdk"},
		{"swift-client-sdk", "LaunchDarkly"},
		{"ios-client-sdk", "LaunchDarkly"},
		{"unknown-sdk-xyz", "unknown-sdk-xyz"}, // unknown falls back to SDK ID
	}
	for _, tt := range tests {
		t.Run(tt.sdkID, func(t *testing.T) {
			args, pkg := InstallArgs(tt.sdkID, "")
			assert.Nil(t, args, "expected nil args for manual SDK %s", tt.sdkID)
			assert.Equal(t, tt.wantPkg, pkg)
		})
	}
}

func TestPackageInstaller_Install_Success(t *testing.T) {
	var capturedDir string
	var capturedArgs []string

	installer := PackageInstaller{
		run: func(dir string, args []string) ([]byte, error) {
			capturedDir = dir
			capturedArgs = args
			return []byte("added 1 package"), nil
		},
	}

	result, err := installer.Install("/my/project", &DetectResult{
		SDKID:          "node-server",
		PackageManager: "npm",
	})

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "node-server", result.SDKID)
	assert.Equal(t, "@launchdarkly/node-server-sdk", result.Package)
	assert.Equal(t, "npm install @launchdarkly/node-server-sdk", result.Command)
	assert.Equal(t, "/my/project", capturedDir)
	assert.Equal(t, []string{"npm", "install", "@launchdarkly/node-server-sdk"}, capturedArgs)
}

func TestPackageInstaller_Install_CommandFailure(t *testing.T) {
	installer := PackageInstaller{
		run: func(dir string, args []string) ([]byte, error) {
			return []byte("npm ERR! not found"), errors.New("exit status 1")
		},
	}

	_, err := installer.Install("/tmp", &DetectResult{
		SDKID:          "node-server",
		PackageManager: "npm",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "npm install @launchdarkly/node-server-sdk")
	assert.Contains(t, err.Error(), "npm ERR! not found")
}

func TestPackageInstaller_Install_ManualSDK_ReturnsNoError(t *testing.T) {
	installer := PackageInstaller{}

	result, err := installer.Install("/tmp", &DetectResult{SDKID: "java-server-sdk"})

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Equal(t, "java-server-sdk", result.SDKID)
	assert.Empty(t, result.Command)
}

func TestPackageInstaller_Install_UnknownSDK_ReturnsError(t *testing.T) {
	installer := PackageInstaller{}

	_, err := installer.Install("/tmp", &DetectResult{SDKID: "totally-unknown-sdk"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown SDK")
}

func TestPackageInstaller_Install_DefaultRunner_UsedWhenNil(t *testing.T) {
	// PackageInstaller{} (zero value) should not panic — it uses execRun.
	// We test this by using a manual SDK so no real command is executed.
	installer := PackageInstaller{}

	result, err := installer.Install("/tmp", &DetectResult{SDKID: "android"})

	require.NoError(t, err)
	assert.False(t, result.Success)
}
