package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallResult contains the outcome of installing an SDK package.
type InstallResult struct {
	SDKID            string `json:"sdk_id"`
	Package          string `json:"package"`
	Version          string `json:"version"`
	Command          string `json:"command"`
	DryRun           bool   `json:"dry_run,omitempty"`
	AlreadyInstalled bool   `json:"already_installed,omitempty"`
	Failed           bool   `json:"failed,omitempty"`
	Success          bool   `json:"success"`
}

// RequiresManualInstall reports whether the SDK has no automated package-manager
// command and must be added by hand (e.g. Java, Android, Swift).
func RequiresManualInstall(sdkID string) bool {
	return manualInstallSDKs[sdkID]
}

// Installer runs the appropriate package manager command to add an SDK dependency.
type Installer interface {
	Install(dir string, detection *DetectResult) (*InstallResult, error)
}

// StubInstaller is a placeholder implementation. Replace with real install logic.
type StubInstaller struct{}

var _ Installer = StubInstaller{}

func (StubInstaller) Install(_ string, _ *DetectResult) (*InstallResult, error) {
	return nil, errors.New("install is not yet implemented: a real Installer must be provided")
}

// PackageInstaller implements Installer using the system package manager.
// Its run field can be replaced in tests to avoid executing real commands.
type PackageInstaller struct {
	run func(dir string, args []string) ([]byte, error)
}

var _ Installer = PackageInstaller{}

// manualInstallSDKs lists SDKs that have no automated package-manager command
// (Java, Android, Swift) but ARE recognised. For these, Install returns
// Success=false without an error so the wizard can proceed and show the package
// identifier. An SDK ID that is neither installable nor in this set is unknown
// and is treated as an error rather than a silent no-op.
var manualInstallSDKs = map[string]bool{
	"java-server-sdk":    true,
	"android":            true,
	"android-client-sdk": true,
	"swift-client-sdk":   true,
	"ios-client-sdk":     true,
}

// Install runs the appropriate package manager command to add the SDK dependency.
// For SDKs that require manual installation (e.g. Java, Android, Swift), Install
// returns a result with Success=false without returning an error. An unknown SDK
// ID returns an error.
func (p PackageInstaller) Install(dir string, detection *DetectResult) (*InstallResult, error) {
	args, pkg := InstallArgs(detection.SDKID, detection.PackageManager)
	if len(args) == 0 {
		if !manualInstallSDKs[detection.SDKID] {
			return nil, fmt.Errorf("unknown SDK %q: no install command available; specify a supported --sdk-id", detection.SDKID)
		}
		return &InstallResult{
			SDKID:   detection.SDKID,
			Package: pkg,
			Success: false,
		}, nil
	}

	// Skip the install if the SDK is already a dependency of the project.
	if IsInstalled(dir, detection.SDKID) {
		return &InstallResult{
			SDKID:            detection.SDKID,
			Package:          pkg,
			AlreadyInstalled: true,
			Success:          true,
		}, nil
	}

	runner := p.run
	if runner == nil {
		runner = execRun
	}

	out, err := runner(dir, args)
	command := strings.Join(args, " ")
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", command, err, strings.TrimSpace(string(out)))
	}
	return &InstallResult{
		SDKID:   detection.SDKID,
		Package: pkg,
		Command: command,
		Success: true,
	}, nil
}

func execRun(dir string, args []string) ([]byte, error) {
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// InstallArgs returns the command-line arguments and package name for installing the given SDK.
// Returns nil args for SDKs that require manual installation (e.g. Java, Android, Swift).
// packageManager is used for Node.js SDKs; for other runtimes the appropriate tool is chosen automatically.
func InstallArgs(sdkID, packageManager string) (args []string, pkg string) {
	switch sdkID {
	case "react-client-sdk":
		pkg = "launchdarkly-react-client-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "react-native":
		pkg = "launchdarkly-react-native-client-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "node-server":
		pkg = "@launchdarkly/node-server-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "js-client-sdk":
		pkg = "@launchdarkly/js-client-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "python-server-sdk":
		pm := packageManager
		if pm == "" {
			pm = "pip"
		}
		pkg = "launchdarkly-server-sdk"
		return []string{pm, "install", pkg}, pkg
	case "go-server-sdk":
		pkg = "github.com/launchdarkly/go-server-sdk/v7"
		return []string{"go", "get", pkg}, pkg
	case "ruby-server-sdk":
		pkg = "launchdarkly-server-sdk"
		return []string{"gem", "install", pkg}, pkg
	case "dotnet-server-sdk":
		pkg = "LaunchDarkly.ServerSdk"
		return []string{"dotnet", "add", "package", pkg}, pkg
	// SDKs requiring manual installation — return a meaningful package identifier
	// so callers can display what the user needs to add.
	case "java-server-sdk":
		return nil, "com.launchdarkly:launchdarkly-java-server-sdk"
	case "android", "android-client-sdk":
		return nil, "com.launchdarkly:launchdarkly-android-client-sdk"
	case "swift-client-sdk", "ios-client-sdk":
		return nil, "LaunchDarkly" // Swift Package Manager / CocoaPods
	default:
		return nil, sdkID
	}
}

// nodeInstallCmd returns the install command arguments for a Node.js package manager.
func nodeInstallCmd(pm, pkg string) []string {
	switch pm {
	case "yarn":
		return []string{"yarn", "add", pkg}
	case "pnpm":
		return []string{"pnpm", "add", pkg}
	case "bun":
		return []string{"bun", "add", pkg}
	default:
		return []string{"npm", "install", pkg}
	}
}

// resolveNodePM normalises the package manager name, defaulting to "npm".
func resolveNodePM(pm string) string {
	switch pm {
	case "yarn", "pnpm", "bun":
		return pm
	default:
		return "npm"
	}
}

// IsInstalled reports whether the SDK is already a dependency of the project in
// dir, by looking for its package identifier in the relevant manifest(s). Only
// covers SDKs with an automated install command; returns false for manual SDKs
// and unknowns.
func IsInstalled(dir, sdkID string) bool {
	_, pkg := InstallArgs(sdkID, "")
	if pkg == "" {
		return false
	}

	var manifests []string
	switch sdkID {
	case "react-client-sdk", "react-native", "node-server", "js-client-sdk":
		manifests = []string{"package.json"}
	case "go-server-sdk":
		manifests = []string{"go.mod", "go.sum"}
	case "python-server-sdk":
		manifests = []string{"requirements.txt", "pyproject.toml", "setup.py"}
	case "ruby-server-sdk":
		manifests = []string{"Gemfile", "Gemfile.lock"}
	case "dotnet-server-sdk":
		matches, _ := filepath.Glob(filepath.Join(dir, "*.csproj"))
		for _, f := range matches {
			if fileContains(f, pkg) {
				return true
			}
		}
		return false
	default:
		return false
	}

	for _, mf := range manifests {
		if fileContains(filepath.Join(dir, mf), pkg) {
			return true
		}
	}
	return false
}

func fileContains(path, substr string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), substr)
}
