package setup

import (
	"errors"
	"os"
	"path/filepath"
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
		{"react-native", "npm", "npm", "@launchdarkly/react-native-client-sdk"},
		{"react-native", "bun", "bun", "@launchdarkly/react-native-client-sdk"},
		{"node-server", "npm", "npm", "@launchdarkly/node-server-sdk"},
		{"node-server", "yarn", "yarn", "@launchdarkly/node-server-sdk"},
		{"node-server", "pnpm", "pnpm", "@launchdarkly/node-server-sdk"},
		{"node-server", "bun", "bun", "@launchdarkly/node-server-sdk"},
		{"node-server", "", "npm", "@launchdarkly/node-server-sdk"},
		{"js-client-sdk", "npm", "npm", "launchdarkly-js-client-sdk"},
		{"js-client-sdk", "bun", "bun", "launchdarkly-js-client-sdk"},
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
	tests := []struct {
		packageManager string
		want           []string
	}{
		// IsInstalled calls InstallArgs with no package manager.
		{"", []string{"pip", "install", "launchdarkly-server-sdk"}},
		{"pip", []string{"pip", "install", "launchdarkly-server-sdk"}},
		{"poetry", []string{"poetry", "add", "launchdarkly-server-sdk"}},
		{"uv", []string{"uv", "add", "launchdarkly-server-sdk"}},
		{"pipenv", []string{"pipenv", "install", "launchdarkly-server-sdk"}},
		// Unrecognised values fall back to pip rather than being run as a command.
		{"conda", []string{"pip", "install", "launchdarkly-server-sdk"}},
	}
	for _, tt := range tests {
		t.Run(tt.packageManager, func(t *testing.T) {
			args, pkg := InstallArgs("python-server-sdk", tt.packageManager)
			assert.Equal(t, tt.want, args)
			assert.Equal(t, "launchdarkly-server-sdk", pkg)
		})
	}
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

// A Gemfile means Bundler owns the project's gems, so the SDK must be added to the
// Gemfile; `gem install` would leave the app unable to require it under bundler.
func TestInstallArgs_Ruby_Bundler(t *testing.T) {
	args, pkg := InstallArgs("ruby-server-sdk", "bundle")
	assert.Equal(t, []string{"bundle", "add", "launchdarkly-server-sdk"}, args)
	assert.Equal(t, "launchdarkly-server-sdk", pkg)
}

func TestInstallArgs_Android_BothSpellings(t *testing.T) {
	for _, id := range []string{"android", "android-client-sdk"} {
		args, pkg := InstallArgs(id, "gradle")
		assert.Nil(t, args, "Android has no automated install command")
		assert.Equal(t, "com.launchdarkly:launchdarkly-android-client-sdk", pkg)
		assert.True(t, RequiresManualInstall(id))
	}
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

func TestPackageInstaller_Install_AlreadyInstalled_SkipsCommand(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "package.json"),
		[]byte(`{"dependencies":{"@launchdarkly/node-server-sdk":"^9.0.0"}}`), 0644))

	installer := PackageInstaller{
		run: func(_ string, _ []string) ([]byte, error) {
			t.Fatal("package manager must not run when the SDK is already installed")
			return nil, nil
		},
	}

	result, err := installer.Install(dir, &DetectResult{SDKID: "node-server", PackageManager: "npm"})

	require.NoError(t, err)
	assert.True(t, result.AlreadyInstalled)
	assert.True(t, result.Success)
	assert.Empty(t, result.Command)
}

func TestRequiresManualInstall(t *testing.T) {
	assert.True(t, RequiresManualInstall("java-server-sdk"))
	assert.True(t, RequiresManualInstall("swift-client-sdk"))
	assert.False(t, RequiresManualInstall("node-server"))
	assert.False(t, RequiresManualInstall("ruby-server-sdk"))
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

// A related package that starts with the SDK's name is not the SDK. Treating it as
// installed skips the install and leaves the integration package without the SDK
// it depends on.
func TestIsInstalled_RelatedPackageIsNotTheSDK(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		content  string
		sdkID    string
		want     bool
	}{
		{"node redis integration only", "package.json",
			`{"dependencies":{"@launchdarkly/node-server-sdk-redis":"^4.0.0"}}`, "node-server", false},
		{"node sdk present", "package.json",
			`{"dependencies":{"@launchdarkly/node-server-sdk":"^9.13.0"}}`, "node-server", true},
		{"node sdk alongside integration", "package.json",
			`{"dependencies":{"@launchdarkly/node-server-sdk":"^9.13.0","@launchdarkly/node-server-sdk-redis":"^4.0.0"}}`, "node-server", true},
		{"python otel plugin only", "requirements.txt",
			"launchdarkly-server-sdk-otel==1.0.0\n", "python-server-sdk", false},
		{"python sdk pinned", "requirements.txt",
			"launchdarkly-server-sdk==9.16.1\n", "python-server-sdk", true},
		{"ruby sdk in gemfile", "Gemfile",
			"gem 'launchdarkly-server-sdk', '~> 8.14'\n", "ruby-server-sdk", true},
		{"ruby related gem only", "Gemfile",
			"gem 'launchdarkly-server-sdk-redis-store'\n", "ruby-server-sdk", false},
		{"go module in go.mod", "go.mod",
			"require github.com/launchdarkly/go-server-sdk/v7 v7.15.5\n", "go-server-sdk", true},
		{"go sdk name as a prefix", "go.mod",
			"require github.com/launchdarkly/go-server-sdk/v7-fork v1.0.0\n", "go-server-sdk", false},
		{"dotnet telemetry package only", "App.csproj",
			`<PackageReference Include="LaunchDarkly.ServerSdk.Telemetry" Version="1.0.0" />`, "dotnet-server-sdk", false},
		{"dotnet sdk present", "App.csproj",
			`<PackageReference Include="LaunchDarkly.ServerSdk" Version="8.16.0" />`, "dotnet-server-sdk", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.manifest), []byte(tt.content), 0600))

			assert.Equal(t, tt.want, IsInstalled(dir, tt.sdkID))
		})
	}
}

// Detection accepts a solution with no project file beside it, so the install has
// to find the project the solution refers to.
func TestIsInstalled_Dotnet_FindsNestedProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.sln"), []byte(""), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src/MyApp"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/MyApp/MyApp.csproj"),
		[]byte(`<PackageReference Include="LaunchDarkly.ServerSdk" Version="8.16.0" />`), 0600))

	assert.True(t, IsInstalled(dir, "dotnet-server-sdk"))
}

func TestInstall_Dotnet_SolutionLayout_TargetsTheProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.sln"), []byte(""), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src/MyApp"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/MyApp/MyApp.csproj"), []byte("<Project/>"), 0600))

	var got []string
	installer := PackageInstaller{run: func(_ string, args []string) ([]byte, error) {
		got = args
		return nil, nil
	}}

	result, err := installer.Install(dir, &DetectResult{SDKID: "dotnet-server-sdk", PackageManager: "dotnet"})

	require.NoError(t, err)
	assert.True(t, result.Success)
	// A bare `dotnet add package` fails when the working directory holds no project.
	assert.Equal(t, []string{"dotnet", "add", "package", "LaunchDarkly.ServerSdk",
		"--project", filepath.Join("src", "MyApp", "MyApp.csproj")}, got)
}

func TestInstall_Dotnet_SingleRootProject_RunsBareCommand(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.csproj"), []byte("<Project/>"), 0600))

	var got []string
	installer := PackageInstaller{run: func(_ string, args []string) ([]byte, error) {
		got = args
		return nil, nil
	}}

	_, err := installer.Install(dir, &DetectResult{SDKID: "dotnet-server-sdk", PackageManager: "dotnet"})

	require.NoError(t, err)
	assert.Equal(t, []string{"dotnet", "add", "package", "LaunchDarkly.ServerSdk"}, got)
}

// Adding the SDK to an arbitrary assembly is worse than saying which projects exist.
func TestInstall_Dotnet_SeveralProjects_ReportsWhyItStopped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.sln"), []byte(""), 0600))
	for _, p := range []string{"src/Api/Api.csproj", "src/Worker/Worker.csproj"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte("<Project/>"), 0600))
	}

	ran := false
	installer := PackageInstaller{run: func(_ string, _ []string) ([]byte, error) {
		ran = true
		return nil, nil
	}}

	result, err := installer.Install(dir, &DetectResult{SDKID: "dotnet-server-sdk", PackageManager: "dotnet"})

	require.NoError(t, err)
	assert.False(t, ran, "a command that cannot succeed must not run")
	assert.True(t, result.Failed)
	assert.False(t, result.Success)
	assert.Contains(t, result.FailureReason, "--project")
	assert.Equal(t, "LaunchDarkly.ServerSdk", result.Package)
}

func TestInstall_Dotnet_NoProjectAtAll_ReportsWhyItStopped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.sln"), []byte(""), 0600))

	result, err := PackageInstaller{run: func(_ string, _ []string) ([]byte, error) {
		t.Fatal("install must not run without a project")
		return nil, nil
	}}.Install(dir, &DetectResult{SDKID: "dotnet-server-sdk", PackageManager: "dotnet"})

	require.NoError(t, err)
	assert.True(t, result.Failed)
	assert.Contains(t, result.FailureReason, "no .csproj")
}

// Build output can hold copies of project files and is large enough to matter.
func TestCsprojFiles_SkipsBuildOutput(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.sln"), []byte(""), 0600))
	for _, p := range []string{"src/MyApp/MyApp.csproj", "src/MyApp/obj/Copy.csproj", "bin/Debug/Stale.csproj"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, p), []byte("<Project/>"), 0600))
	}

	assert.Equal(t, []string{filepath.Join(dir, "src/MyApp/MyApp.csproj")}, csprojFiles(dir))
}

// The templates import the package InstallArgs installs; a mismatch means the user
// installs one package and the snippet requires another. These are the pairs where
// LaunchDarkly ships both a scoped and an unscoped package for the same SDK.
func TestInstallArgs_PackageMatchesTemplateImport(t *testing.T) {
	tests := []struct {
		sdkID      string
		wantImport string
	}{
		{"node-server", "@launchdarkly/node-server-sdk"},
		{"react-client-sdk", "launchdarkly-react-client-sdk"},
		{"react-native", "@launchdarkly/react-native-client-sdk"},
		{"js-client-sdk", "launchdarkly-js-client-sdk"},
	}
	for _, tt := range tests {
		t.Run(tt.sdkID, func(t *testing.T) {
			_, pkg := InstallArgs(tt.sdkID, "npm")
			assert.Equal(t, tt.wantImport, pkg)

			rendered, err := RenderTemplate(tt.sdkID, InitConfig{})
			require.NoError(t, err)
			assert.Contains(t, rendered, "'"+tt.wantImport+"'",
				"template must import the package we install")
		})
	}
}
