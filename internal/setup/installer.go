package setup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	// FailureReason carries the underlying error when Failed is true, so callers
	// can tell the user why the automatic install did not run.
	FailureReason string `json:"failure_reason,omitempty"`
	Success       bool   `json:"success"`
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

	if detection.SDKID == "dotnet-server-sdk" {
		target, reason := dotnetProjectArg(dir)
		if reason != "" {
			return &InstallResult{
				SDKID:         detection.SDKID,
				Package:       pkg,
				Failed:        true,
				FailureReason: reason,
			}, nil
		}
		args = append(args, target...)
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

// dotnetProjectArg returns the extra arguments needed to point `dotnet add
// package` at a project, or a reason the install cannot run unattended. A bare
// `dotnet add package` only works when the working directory holds exactly one
// project file, but detection also accepts a solution whose projects live in
// subdirectories.
func dotnetProjectArg(dir string) (args []string, reason string) {
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.csproj")); len(matches) == 1 {
		return nil, ""
	}
	projects := csprojFiles(dir)
	switch len(projects) {
	case 0:
		return nil, "no .csproj file found; add LaunchDarkly.ServerSdk to your project manually"
	case 1:
		rel, err := filepath.Rel(dir, projects[0])
		if err != nil {
			rel = projects[0]
		}
		return []string{"--project", rel}, ""
	default:
		// Picking one of several projects would add the SDK to an arbitrary
		// assembly, so let the user say which.
		return nil, fmt.Sprintf("found %d projects in this solution; run `dotnet add package LaunchDarkly.ServerSdk --project <path>` for the one that needs the SDK", len(projects))
	}
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
		pkg = "@launchdarkly/react-native-client-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "node-server":
		pkg = "@launchdarkly/node-server-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "js-client-sdk":
		// The unscoped v3 package, whose initialize API the init template and the
		// quickstart instructions both use. The scoped @launchdarkly/js-client-sdk is
		// v4 and exposes createClient instead.
		pkg = "launchdarkly-js-client-sdk"
		return nodeInstallCmd(resolveNodePM(packageManager), pkg), pkg
	case "python-server-sdk":
		pkg = "launchdarkly-server-sdk"
		return pythonInstallCmd(packageManager, pkg), pkg
	case "go-server-sdk":
		pkg = "github.com/launchdarkly/go-server-sdk/v7"
		return []string{"go", "get", pkg}, pkg
	case "ruby-server-sdk":
		pkg = "launchdarkly-server-sdk"
		// Bundler-managed projects need the gem recorded in the Gemfile; a bare
		// `gem install` would succeed without making the SDK available to the app.
		if packageManager == "bundle" {
			return []string{"bundle", "add", pkg}, pkg
		}
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

// pythonInstallCmd returns the install command arguments for a Python package
// manager. Anything unrecognised — including the empty string, which IsInstalled
// passes — falls back to pip.
func pythonInstallCmd(pm, pkg string) []string {
	switch pm {
	case "poetry":
		return []string{"poetry", "add", pkg}
	case "uv":
		return []string{"uv", "add", pkg}
	case "pipenv":
		return []string{"pipenv", "install", pkg}
	default:
		return []string{"pip", "install", pkg}
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
		manifests = []string{"requirements.txt", "pyproject.toml", "setup.py", "Pipfile", "uv.lock"}
	case "ruby-server-sdk":
		manifests = []string{"Gemfile", "Gemfile.lock"}
	case "dotnet-server-sdk":
		for _, f := range csprojFiles(dir) {
			if fileMentionsPackage(f, pkg) {
				return true
			}
		}
		return false
	default:
		return false
	}

	for _, mf := range manifests {
		if fileMentionsPackage(filepath.Join(dir, mf), pkg) {
			return true
		}
	}
	return false
}

func fileMentionsPackage(path, pkg string) bool {
	b, err := os.ReadFile(path)
	return err == nil && mentionsPackage(string(b), pkg)
}

// mentionsPackage reports whether content names pkg as a whole dependency rather
// than as the prefix of a longer name. A plain substring test treats
// @launchdarkly/node-server-sdk-redis as proof that @launchdarkly/node-server-sdk
// is installed, so setup skips installing the SDK the integration package needs.
// Every manifest format delimits a dependency name with a quote, whitespace, or a
// comparison operator, so requiring a non-name character on both sides works for
// all of them without parsing each one.
func mentionsPackage(content, pkg string) bool {
	for i := 0; ; {
		at := strings.Index(content[i:], pkg)
		if at < 0 {
			return false
		}
		at += i
		end := at + len(pkg)
		beforeOK := at == 0 || !isPackageNameChar(rune(content[at-1]))
		afterOK := end == len(content) || !isPackageNameChar(rune(content[end]))
		if beforeOK && afterOK {
			return true
		}
		i = at + 1
	}
}

// isPackageNameChar reports whether r can appear inside a package name, and so
// whether it continues a name rather than terminating one.
func isPackageNameChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '-', r == '_', r == '.', r == '/', r == '@':
		return true
	}
	return false
}

// csprojFiles returns the project files to consider for a .NET project, preferring
// those in dir. Detection accepts a solution with no project file beside it, so
// fall back to searching for the projects the solution refers to.
func csprojFiles(dir string) []string {
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.csproj")); len(matches) > 0 {
		return matches
	}
	var found []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Build output holds copies of nothing useful and can be large.
			if name := d.Name(); name == "bin" || name == "obj" || name == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".csproj") {
			found = append(found, path)
		}
		return nil
	})
	sort.Strings(found)
	return found
}
