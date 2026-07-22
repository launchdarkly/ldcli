package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// DetectResult contains information about the user's project detected from the working directory.
type DetectResult struct {
	Language       string `json:"language"`
	Framework      string `json:"framework,omitempty"`
	PackageManager string `json:"package_manager"`
	SDKID          string `json:"sdk_id"`
	EntryPoint     string `json:"entry_point"`
}

// Detector inspects a directory to determine the language, framework, package manager,
// recommended SDK, and entry point file.
type Detector interface {
	Detect(dir string) (*DetectResult, error)
}

// StubDetector is a placeholder implementation. Replace with real detection logic.
type StubDetector struct{}

var _ Detector = StubDetector{}

func (StubDetector) Detect(_ string) (*DetectResult, error) {
	return nil, errors.New("detect is not yet implemented: a real Detector must be provided")
}

// FileDetector implements Detector by scanning the filesystem for known project indicators.
type FileDetector struct{}

var _ Detector = FileDetector{}

// Detect scans dir for known project files and returns a DetectResult with language,
// framework, SDK ID, package manager, and a suggested entry point file.
// Returns an error if the project type cannot be determined.
func (FileDetector) Detect(dir string) (*DetectResult, error) {
	if result := detectNode(dir); result != nil {
		return result, nil
	}
	if result := detectGo(dir); result != nil {
		return result, nil
	}
	if result := detectPython(dir); result != nil {
		return result, nil
	}
	if result := detectRuby(dir); result != nil {
		return result, nil
	}
	if result := detectJava(dir); result != nil {
		return result, nil
	}
	if result := detectSwift(dir); result != nil {
		return result, nil
	}
	if result := detectDotnet(dir); result != nil {
		return result, nil
	}
	return nil, errors.New("could not detect project language from directory; try specifying --sdk-id manually")
}

func detectNode(dir string) *DetectResult {
	pkgBytes, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(pkgBytes, &pkg) != nil {
		return nil
	}

	allDeps := make(map[string]string, len(pkg.Dependencies)+len(pkg.DevDependencies))
	for k, v := range pkg.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pkg.DevDependencies {
		allDeps[k] = v
	}

	pm := detectNodePM(dir)

	// Next.js apps run a Node server (SSR and API routes), so server-side flag
	// evaluation uses the Node server SDK rather than a browser client SDK.
	if _, ok := allDeps["next"]; ok {
		return &DetectResult{
			Language:       "JavaScript",
			Framework:      "Next.js",
			PackageManager: pm,
			SDKID:          "node-server",
			EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
				"src/index.ts", "src/index.js",
				"pages/index.tsx", "pages/index.ts", "pages/index.js",
				"index.js",
			})),
		}
	}

	if _, ok := allDeps["react-native"]; ok {
		return &DetectResult{
			Language:       "JavaScript",
			Framework:      "React Native",
			PackageManager: pm,
			SDKID:          "react-native",
			EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
				"src/App.tsx", "src/App.jsx", "src/App.js",
				"src/index.tsx", "src/index.jsx", "src/index.js",
				"index.js",
			})),
		}
	}
	if _, ok := allDeps["react"]; ok {
		return &DetectResult{
			Language:       "JavaScript",
			Framework:      "React",
			PackageManager: pm,
			SDKID:          "react-client-sdk",
			EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
				"src/App.tsx", "src/App.jsx", "src/App.js",
				"src/index.tsx", "src/index.jsx", "src/index.js",
				"index.js",
			})),
		}
	}
	jsClientFrameworks := []struct{ dep, framework string }{
		{"backbone", "Backbone"},
		{"svelte", "Svelte"},
		{"vue", "Vue"},
		{"@angular/core", "Angular"},
		{"ember-source", "Ember"},
		{"preact", "Preact"},
	}
	for _, fw := range jsClientFrameworks {
		if _, ok := allDeps[fw.dep]; ok {
			return &DetectResult{
				Language:       "JavaScript",
				Framework:      fw.framework,
				PackageManager: pm,
				SDKID:          "js-client-sdk",
				EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
					"src/App.tsx", "src/App.jsx", "src/App.js",
					"src/index.tsx", "src/index.jsx", "src/index.js",
					"src/main.ts", "src/main.js", "index.js",
				})),
			}
		}
	}

	return &DetectResult{
		Language:       "JavaScript",
		PackageManager: pm,
		SDKID:          "node-server",
		EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
			"src/index.ts", "src/index.js",
			"index.ts", "index.js",
			"server.ts", "server.js",
			"app.ts", "app.js",
		})),
	}
}

func detectNodePM(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	if _, err := os.Stat(filepath.Join(dir, "bun.lock")); err == nil {
		return "bun"
	}
	return "npm"
}

func detectGo(dir string) *DetectResult {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return nil
	}
	return &DetectResult{
		Language:       "Go",
		PackageManager: "go",
		SDKID:          "go-server-sdk",
		EntryPoint:     filepath.Join(dir, firstExistingIn(dir, []string{"cmd/main.go", "main.go"})),
	}
}

func detectPython(dir string) *DetectResult {
	for _, indicator := range []string{"requirements.txt", "pyproject.toml", "setup.py"} {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			return &DetectResult{
				Language:       "Python",
				PackageManager: "pip",
				SDKID:          "python-server-sdk",
				EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
					"src/main.py", "manage.py", "app.py", "main.py",
				})),
			}
		}
	}
	return nil
}

func detectRuby(dir string) *DetectResult {
	found := false
	for _, indicator := range []string{"Gemfile", "Gemfile.lock", "config.ru"} {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			found = true
			break
		}
	}
	if !found {
		if matches, _ := filepath.Glob(filepath.Join(dir, "*.gemspec")); len(matches) == 0 {
			return nil
		}
	}
	return &DetectResult{
		Language:       "Ruby",
		PackageManager: "gem",
		SDKID:          "ruby-server-sdk",
		EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
			"config.ru", "app.rb", "main.rb",
		})),
	}
}

func detectJava(dir string) *DetectResult {
	for _, indicator := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			pm := "gradle"
			if indicator == "pom.xml" {
				pm = "mvn"
			}
			// Android projects use Gradle but are distinguished by AndroidManifest.xml.
			for _, manifest := range []string{
				"app/src/main/AndroidManifest.xml",
				"src/main/AndroidManifest.xml",
			} {
				if _, err := os.Stat(filepath.Join(dir, manifest)); err == nil {
					return &DetectResult{
						Language:       "Java",
						PackageManager: "gradle",
						SDKID:          "android-client-sdk",
						EntryPoint:     filepath.Join(dir, "app/src/main/java/MainActivity.java"),
					}
				}
			}
			return &DetectResult{
				Language:       "Java",
				PackageManager: pm,
				SDKID:          "java-server-sdk",
				EntryPoint:     filepath.Join(dir, "src/main/java/Main.java"),
			}
		}
	}
	return nil
}

func detectSwift(dir string) *DetectResult {
	pm := "spm"
	if _, err := os.Stat(filepath.Join(dir, "Podfile")); err == nil {
		pm = "cocoapods"
	}
	indicators := []string{"Package.swift", "Podfile"}
	for _, f := range indicators {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return &DetectResult{
				Language:       "Swift",
				PackageManager: pm,
				SDKID:          "swift-client-sdk",
				EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
					"Sources/main.swift", "App.swift", "ContentView.swift", "AppDelegate.swift",
				})),
			}
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.xcodeproj"))
	if len(matches) > 0 {
		return &DetectResult{
			Language:       "Swift",
			PackageManager: pm,
			SDKID:          "swift-client-sdk",
			EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
				"Sources/main.swift", "App.swift", "ContentView.swift", "AppDelegate.swift",
			})),
		}
	}
	return nil
}

func detectDotnet(dir string) *DetectResult {
	for _, pattern := range []string{"*.csproj", "*.sln"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(matches) > 0 {
			return &DetectResult{
				Language:       "C#",
				PackageManager: "dotnet",
				SDKID:          "dotnet-server-sdk",
				EntryPoint: filepath.Join(dir, firstExistingIn(dir, []string{
					"Program.cs", "Startup.cs", "src/Program.cs",
				})),
			}
		}
	}
	return nil
}

// SDKOption describes a LaunchDarkly SDK available for use with ldcli setup.
type SDKOption struct {
	ID       string
	Language string
	Name     string
}

// KnownSDKs is the ordered list of SDKs available for manual selection when
// auto-detection fails or the user wants to override the detected SDK.
var KnownSDKs = []SDKOption{
	{ID: "node-server", Language: "JavaScript", Name: "Node.js"},
	{ID: "react-client-sdk", Language: "JavaScript", Name: "React"},
	{ID: "react-native", Language: "JavaScript", Name: "React Native"},
	{ID: "js-client-sdk", Language: "JavaScript", Name: "JavaScript (Browser)"},
	{ID: "python-server-sdk", Language: "Python", Name: "Python"},
	{ID: "go-server-sdk", Language: "Go", Name: "Go"},
	{ID: "java-server-sdk", Language: "Java", Name: "Java"},
	{ID: "android-client-sdk", Language: "Java", Name: "Android"},
	{ID: "dotnet-server-sdk", Language: "C#", Name: ".NET"},
	{ID: "swift-client-sdk", Language: "Swift", Name: "iOS/Swift"},
	{ID: "ruby-server-sdk", Language: "Ruby", Name: "Ruby"},
}

// firstExistingIn returns the first candidate that exists as a file in dir,
// or the last candidate if none exist (as a suggested path).
// Returns an empty string if candidates is empty.
func firstExistingIn(dir string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(dir, c)); err == nil {
			return c
		}
	}
	return candidates[len(candidates)-1]
}
