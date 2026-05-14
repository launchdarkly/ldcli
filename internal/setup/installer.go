package setup

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// InstallResult contains the outcome of installing an SDK package.
type InstallResult struct {
	SDKID   string `json:"sdk_id"`
	Package string `json:"package"`
	Version string `json:"version"`
	Command string `json:"command"`
	Success bool   `json:"success"`
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

// Install runs the appropriate package manager command to add the SDK dependency.
// For SDKs that require manual installation (e.g. Java, Android, Swift), Install
// returns a result with Success=false without returning an error.
func (p PackageInstaller) Install(dir string, detection *DetectResult) (*InstallResult, error) {
	args, pkg := InstallArgs(detection.SDKID, detection.PackageManager)
	if len(args) == 0 {
		return &InstallResult{
			SDKID:   detection.SDKID,
			Package: pkg,
			Success: false,
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
