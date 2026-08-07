package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DetectResult contains information about the user's project detected from the working directory.
type DetectResult struct {
	Language       string `json:"language"`
	Framework      string `json:"framework,omitempty"`
	PackageManager string `json:"package_manager"`
	SDKID          string `json:"sdk_id"`
	EntryPoint     string `json:"entry_point"`
	// EntryPointExists distinguishes an entry point we found from one we merely
	// suggest. Callers must not write initialization code into a suggested path
	// without telling the user, since the project does not load that file.
	EntryPointExists bool `json:"entry_point_exists"`
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
// A root package.json is often only build tooling — Rails with jsbundling, Django
// with Tailwind, a Go binary published to npm — so the backend manifests are
// checked first and Node claims the project only when it is the sole manifest.
func (FileDetector) Detect(dir string) (*DetectResult, error) {
	for _, detect := range []func(string) *DetectResult{
		detectGo,
		detectPython,
		detectRuby,
		detectJava,
		detectSwift,
		detectDotnet,
		detectNode,
	} {
		if result := detect(dir); result != nil {
			return result, nil
		}
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
		// Entry point: https://nextjs.org/docs/app/guides/instrumentation
		// Only the hook is guaranteed to stay out of the browser bundle; a page or
		// route module may carry 'use client' and ship the SDK key to the browser.
		ep, exists := entryPoint(dir, "instrumentation.ts",
			"instrumentation.ts", "instrumentation.js",
			"src/instrumentation.ts", "src/instrumentation.js",
		)
		return &DetectResult{
			Language:         "JavaScript",
			Framework:        "Next.js",
			PackageManager:   pm,
			SDKID:            "node-server",
			EntryPoint:       ep,
			EntryPointExists: exists,
		}
	}

	if _, ok := allDeps["react-native"]; ok {
		// Entry point: https://reactnative.dev/docs/appregistry
		ep, exists := entryPoint(dir, "index.js",
			"src/App.tsx", "src/App.jsx", "src/App.js",
			"src/index.tsx", "src/index.jsx", "src/index.js",
			"App.tsx", "App.js", "index.js",
		)
		return &DetectResult{
			Language:         "JavaScript",
			Framework:        "React Native",
			PackageManager:   pm,
			SDKID:            "react-native",
			EntryPoint:       ep,
			EntryPointExists: exists,
		}
	}
	if _, ok := allDeps["react"]; ok {
		// Vite entry: https://vite.dev/guide/#index-html-and-project-root
		// CRA entry: https://create-react-app.dev/docs/folder-structure
		// Mounting: https://react.dev/reference/react-dom/client/createRoot
		ep, exists := entryPoint(dir, "src/App.tsx",
			"src/App.tsx", "src/App.jsx", "src/App.js",
			"src/main.tsx", "src/main.jsx",
			"src/index.tsx", "src/index.jsx", "src/index.js",
			"index.js",
		)
		return &DetectResult{
			Language:         "JavaScript",
			Framework:        "React",
			PackageManager:   pm,
			SDKID:            "react-client-sdk",
			EntryPoint:       ep,
			EntryPointExists: exists,
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
			// Vite entry: https://vite.dev/guide/#index-html-and-project-root
			// Angular entry: https://angular.dev/reference/configs/file-structure
			ep, exists := entryPoint(dir, "src/main.ts",
				"src/App.tsx", "src/App.jsx", "src/App.js",
				"src/index.tsx", "src/index.jsx", "src/index.js",
				"src/main.ts", "src/main.js", "index.js",
			)
			return &DetectResult{
				Language:         "JavaScript",
				Framework:        fw.framework,
				PackageManager:   pm,
				SDKID:            "js-client-sdk",
				EntryPoint:       ep,
				EntryPointExists: exists,
			}
		}
	}

	// Entry point: https://docs.npmjs.com/cli/v11/configuring-npm/package-json#main
	// NestJS bootstraps from src/main.ts: https://docs.nestjs.com/first-steps
	ep, exists := entryPoint(dir, "index.js",
		"src/index.ts", "src/index.js",
		"src/main.ts", "src/main.js",
		"index.ts", "index.js",
		"server.ts", "server.js",
		"app.ts", "app.js",
	)
	return &DetectResult{
		Language:         "JavaScript",
		PackageManager:   pm,
		SDKID:            "node-server",
		EntryPoint:       ep,
		EntryPointExists: exists,
	}
}

func detectNodePM(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	// Lockfiles: https://bun.com/docs/install/lockfile
	for _, lock := range []string{"bun.lock", "bun.lockb"} {
		if _, err := os.Stat(filepath.Join(dir, lock)); err == nil {
			return "bun"
		}
	}
	return "npm"
}

func detectGo(dir string) *DetectResult {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return nil
	}
	// Entry point: https://go.dev/ref/spec#Program_execution
	ep, exists := entryPoint(dir, "main.go", "main.go", "cmd/main.go")
	return &DetectResult{
		Language:         "Go",
		PackageManager:   "go",
		SDKID:            "go-server-sdk",
		EntryPoint:       ep,
		EntryPointExists: exists,
	}
}

func detectPython(dir string) *DetectResult {
	for _, indicator := range []string{"requirements.txt", "pyproject.toml", "setup.py", "Pipfile"} {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			// Django entry: https://docs.djangoproject.com/en/stable/ref/django-admin/
			// Flask entry: https://flask.palletsprojects.com/en/stable/quickstart/
			ep, exists := entryPoint(dir, "main.py",
				"src/main.py", "manage.py", "app.py", "main.py",
			)
			return &DetectResult{
				Language:         "Python",
				PackageManager:   detectPythonPM(dir),
				SDKID:            "python-server-sdk",
				EntryPoint:       ep,
				EntryPointExists: exists,
			}
		}
	}
	return nil
}

// detectPythonPM identifies the tool that manages the project's dependencies, so
// callers install into the project rather than running pip against whatever
// interpreter happens to be on PATH.
//
// https://docs.astral.sh/uv/concepts/projects/layout/
// https://pipenv.pypa.io/en/latest/
// https://python-poetry.org/docs/pyproject/
func detectPythonPM(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "uv.lock")); err == nil {
		return "uv"
	}
	if _, err := os.Stat(filepath.Join(dir, "Pipfile")); err == nil {
		return "pipenv"
	}
	if b, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
		if bytes.Contains(b, []byte("[tool.poetry]")) {
			return "poetry"
		}
		if bytes.Contains(b, []byte("[tool.uv]")) {
			return "uv"
		}
	}
	return "pip"
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
	// Gemfile: https://bundler.io/guides/gemfile.html
	pm := "gem"
	if _, err := os.Stat(filepath.Join(dir, "Gemfile")); err == nil {
		pm = "bundle"
	}
	// config.ru: https://github.com/rack/rack/blob/main/SPEC.rdoc
	ep, exists := entryPoint(dir, "main.rb", "config.ru", "app.rb", "main.rb")
	return &DetectResult{
		Language:         "Ruby",
		PackageManager:   pm,
		SDKID:            "ruby-server-sdk",
		EntryPoint:       ep,
		EntryPointExists: exists,
	}
}

func detectJava(dir string) *DetectResult {
	for _, indicator := range []string{"pom.xml", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(dir, indicator)); err == nil {
			pm := "gradle"
			if indicator == "pom.xml" {
				pm = "mvn"
			}
			// Manifest: https://developer.android.com/guide/topics/manifest/manifest-intro
			for _, manifest := range []string{
				"app/src/main/AndroidManifest.xml",
				"src/main/AndroidManifest.xml",
			} {
				if _, err := os.Stat(filepath.Join(dir, manifest)); err != nil {
					continue
				}
				// Entry point: https://developer.android.com/reference/android/app/Activity
				// The activity lives under a package directory, so search for it
				// rather than guessing the package name.
				srcRoot := strings.TrimSuffix(manifest, "/AndroidManifest.xml")
				ep, exists := entryPoint(dir, srcRoot+"/java/MainActivity.kt",
					findFileUnder(dir, srcRoot+"/java", "MainActivity.kt", "MainActivity.java"),
					findFileUnder(dir, srcRoot+"/kotlin", "MainActivity.kt"),
				)
				return &DetectResult{
					Language:         "Java",
					PackageManager:   "gradle",
					SDKID:            "android",
					EntryPoint:       ep,
					EntryPointExists: exists,
				}
			}
			// Gradle layout: https://docs.gradle.org/current/userguide/building_java_projects.html
			// Maven layout: https://maven.apache.org/guides/introduction/introduction-to-the-pom.html
			ep, exists := entryPoint(dir, "src/main/java/Main.java",
				findFileUnder(dir, "src/main/java", "Main.java", "Application.java", "App.java"),
			)
			return &DetectResult{
				Language:         "Java",
				PackageManager:   pm,
				SDKID:            "java-server-sdk",
				EntryPoint:       ep,
				EntryPointExists: exists,
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
	swiftEntryPoint := func(appRoot string) (string, bool) {
		return entryPoint(dir, "App.swift", swiftEntryCandidates(dir, appRoot)...)
	}
	indicators := []string{"Package.swift", "Podfile"}
	for _, f := range indicators {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			ep, exists := swiftEntryPoint(xcodeAppRoot(dir))
			return &DetectResult{
				Language:         "Swift",
				PackageManager:   pm,
				SDKID:            "swift-client-sdk",
				EntryPoint:       ep,
				EntryPointExists: exists,
			}
		}
	}
	if appRoot := xcodeAppRoot(dir); appRoot != "" {
		ep, exists := swiftEntryPoint(appRoot)
		return &DetectResult{
			Language:         "Swift",
			PackageManager:   pm,
			SDKID:            "swift-client-sdk",
			EntryPoint:       ep,
			EntryPointExists: exists,
		}
	}
	return nil
}

// swiftEntryCandidates lists entry-point paths to try for a Swift project, most
// specific first. appRoot is the Xcode app directory, empty when there is no Xcode
// project. Any-name matches are confined to a package with a single target, where
// the entry file is named after that target; with several targets there is no way to
// tell an entry point from a helper.
//
// App struct: https://developer.apple.com/documentation/swiftui/app
// Package targets: https://developer.apple.com/documentation/packagedescription/target
func swiftEntryCandidates(dir, appRoot string) []string {
	candidates := []string{
		"App.swift", "ContentView.swift", "AppDelegate.swift",
		findFileUnder(dir, appRoot, "*App.swift", "ContentView.swift", "AppDelegate.swift"),
	}
	// Searching Sources/ at all is confined to a single-target package. Across
	// several targets there is no way to tell an executable's entry file from a
	// library's, so report a suggestion instead of an arbitrary hit.
	if target := soleSubdir(dir, "Sources"); target != "" {
		candidates = append(candidates, findFileUnder(dir, target,
			"main.swift", filepath.Base(target)+".swift", "*App.swift", "*.swift",
		))
	}
	return candidates
}

// soleSubdir returns the path relative to dir of root's only subdirectory, or an
// empty string when root is missing or holds anything other than exactly one.
func soleSubdir(dir, root string) string {
	entries, err := os.ReadDir(filepath.Join(dir, root))
	if err != nil {
		return ""
	}
	var found string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if found != "" {
			return ""
		}
		found = filepath.Join(root, e.Name())
	}
	return found
}

// xcodeAppRoot returns the source directory an Xcode project keeps its app code in,
// which the templates name after the project (MyApp.xcodeproj alongside MyApp/).
// Returns an empty string when dir holds no Xcode project.
//
// https://developer.apple.com/documentation/xcode/creating-an-xcode-project-for-an-app
func xcodeAppRoot(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.xcodeproj"))
	if len(matches) == 0 {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(matches[0]), ".xcodeproj")
}

func detectDotnet(dir string) *DetectResult {
	for _, pattern := range []string{"*.csproj", "*.sln"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(matches) > 0 {
			// Entry point: https://learn.microsoft.com/en-us/aspnet/core/fundamentals/startup
			ep, exists := entryPoint(dir, "Program.cs",
				"Program.cs", "Startup.cs", "src/Program.cs",
			)
			return &DetectResult{
				Language:         "C#",
				PackageManager:   "dotnet",
				SDKID:            "dotnet-server-sdk",
				EntryPoint:       ep,
				EntryPointExists: exists,
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
	{ID: "android", Language: "Java", Name: "Android"},
	{ID: "dotnet-server-sdk", Language: "C#", Name: ".NET"},
	{ID: "swift-client-sdk", Language: "Swift", Name: "iOS/Swift"},
	{ID: "ruby-server-sdk", Language: "Ruby", Name: "Ruby"},
}

// entryPoint returns the first candidate that exists as a file under dir, joined
// to dir, together with true. When no candidate exists it returns fallback joined
// to dir and false, so callers can tell a file we found from one we suggest.
// Empty candidates are skipped, which lets callers pass the result of a lookup
// that may have come up empty.
func entryPoint(dir, fallback string, candidates ...string) (string, bool) {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, c)); err == nil && !info.IsDir() {
			return filepath.Join(dir, c), true
		}
	}
	return filepath.Join(dir, fallback), false
}

// findFileUnder walks root (relative to dir) and returns the first file whose base
// name matches one of names, as a path relative to dir. A name may start with "*"
// to match by suffix, so "*App.swift" finds MyAppApp.swift. Names are tried in
// order so callers can express a preference. Returns an empty string when root is
// missing or contains no match. An empty root yields no match rather than walking
// the whole project.
func findFileUnder(dir, root string, names ...string) string {
	if root == "" {
		return ""
	}
	matches := func(base, name string) bool {
		if suffix, ok := strings.CutPrefix(name, "*"); ok {
			return strings.HasSuffix(base, suffix)
		}
		return base == name
	}
	for _, name := range names {
		var found string
		_ = filepath.WalkDir(filepath.Join(dir, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && matches(d.Name(), name) {
				found = path
				return fs.SkipAll
			}
			return nil
		})
		if found != "" {
			if rel, err := filepath.Rel(dir, found); err == nil {
				return rel
			}
		}
	}
	return ""
}
