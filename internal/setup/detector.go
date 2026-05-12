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
	if result := detectJava(dir); result != nil {
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
		EntryPoint:     filepath.Join(dir, firstExistingIn(dir, []string{"main.go", "cmd/main.go"})),
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
					"main.py", "app.py", "manage.py", "src/main.py",
				})),
			}
		}
	}
	return nil
}

func detectJava(dir string) *DetectResult {
	for _, indicator := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			pm := "gradle"
			if indicator == "pom.xml" {
				pm = "mvn"
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
