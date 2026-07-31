package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDetectFile writes content to a file in dir, creating parent directories as needed.
func writeDetectFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
}

func TestFileDetector_DetectsReact(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0"}}`)
	writeDetectFile(t, dir, "src/App.tsx", "// App")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "react-client-sdk", result.SDKID)
	assert.Equal(t, "JavaScript", result.Language)
	assert.Equal(t, "React", result.Framework)
	assert.Equal(t, "npm", result.PackageManager)
	assert.Equal(t, filepath.Join(dir, "src/App.tsx"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_DetectsReactNative(t *testing.T) {
	dir := t.TempDir()
	// React Native projects always list both "react" and "react-native" as deps;
	// react-native must be checked first so it takes priority over react.
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0","react-native":"^0.73.0"}}`)

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "react-native", result.SDKID)
	assert.Equal(t, "JavaScript", result.Language)
	assert.Equal(t, "React Native", result.Framework)
}

func TestFileDetector_DetectsNextJs(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0","next":"^14.0.0"}}`)

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "node-server", result.SDKID)
	assert.Equal(t, "JavaScript", result.Language)
	assert.Equal(t, "Next.js", result.Framework)
}

func TestFileDetector_DetectsNodeJs(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"express":"^4.0.0"}}`)
	writeDetectFile(t, dir, "index.js", "// entry")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "node-server", result.SDKID)
	assert.Equal(t, "JavaScript", result.Language)
	assert.Empty(t, result.Framework)
	assert.Equal(t, filepath.Join(dir, "index.js"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_DetectsGo(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.21\n")
	writeDetectFile(t, dir, "main.go", "package main\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "go-server-sdk", result.SDKID)
	assert.Equal(t, "Go", result.Language)
	assert.Equal(t, "go", result.PackageManager)
	assert.Equal(t, filepath.Join(dir, "main.go"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_DetectsPython_RequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "requirements.txt", "flask==3.0.0\n")
	writeDetectFile(t, dir, "app.py", "# app")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "python-server-sdk", result.SDKID)
	assert.Equal(t, "Python", result.Language)
	assert.Equal(t, "pip", result.PackageManager)
	assert.Equal(t, filepath.Join(dir, "app.py"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_DetectsPython_Pyproject(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "pyproject.toml", "[tool.poetry]\nname = \"myapp\"\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "python-server-sdk", result.SDKID)
}

func TestFileDetector_DetectsJava_PomXml(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "pom.xml", "<project></project>")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "java-server-sdk", result.SDKID)
	assert.Equal(t, "Java", result.Language)
	assert.Equal(t, "mvn", result.PackageManager)
}

func TestFileDetector_DetectsJava_BuildGradle(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle", "plugins { id 'java' }")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "java-server-sdk", result.SDKID)
	assert.Equal(t, "gradle", result.PackageManager)
}

func TestFileDetector_DetectsAndroid_BuildGradle(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle", "plugins { id 'com.android.application' }")
	writeDetectFile(t, dir, "app/src/main/AndroidManifest.xml", "<manifest/>")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "android", result.SDKID)
	assert.Equal(t, "Java", result.Language)
	assert.Equal(t, "gradle", result.PackageManager)
}

func TestFileDetector_DetectsAndroid_KotlinDsl(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle.kts", "plugins { id(\"com.android.application\") }")
	writeDetectFile(t, dir, "app/src/main/AndroidManifest.xml", "<manifest/>")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "android", result.SDKID)
	assert.Equal(t, "gradle", result.PackageManager)
}

func TestFileDetector_DetectsJava_NotAndroid(t *testing.T) {
	// build.gradle without AndroidManifest.xml should still return java-server-sdk
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle", "plugins { id 'java' }")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "java-server-sdk", result.SDKID)
}

func TestFileDetector_UnknownProject_ReturnsError(t *testing.T) {
	dir := t.TempDir()

	_, err := FileDetector{}.Detect(dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not detect")
}

func TestFileDetector_DetectsNodePM_Pnpm(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{}`)
	writeDetectFile(t, dir, "pnpm-lock.yaml", "")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "pnpm", result.PackageManager)
}

func TestFileDetector_DetectsNodePM_Yarn(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{}`)
	writeDetectFile(t, dir, "yarn.lock", "")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "yarn", result.PackageManager)
}

func TestFileDetector_DetectsNodePM_Bun(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{}`)
	writeDetectFile(t, dir, "bun.lock", "")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "bun", result.PackageManager)
}

func TestFileDetector_DetectsJsClientFramework(t *testing.T) {
	tests := []struct {
		dep       string
		framework string
	}{
		{"vue", "Vue"},
		{"svelte", "Svelte"},
		{"backbone", "Backbone"},
		{"@angular/core", "Angular"},
		{"ember-source", "Ember"},
		{"preact", "Preact"},
	}
	for _, tt := range tests {
		t.Run(tt.framework, func(t *testing.T) {
			dir := t.TempDir()
			writeDetectFile(t, dir, "package.json", `{"dependencies":{"`+tt.dep+`":"^1.0.0"}}`)

			result, err := FileDetector{}.Detect(dir)

			require.NoError(t, err)
			assert.Equal(t, "js-client-sdk", result.SDKID)
			assert.Equal(t, tt.framework, result.Framework)
		})
	}
}

func TestFileDetector_DetectsSwift_PackageSwift(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "swift-client-sdk", result.SDKID)
	assert.Equal(t, "Swift", result.Language)
	assert.Equal(t, "spm", result.PackageManager)
}

func TestFileDetector_DetectsSwift_Podfile(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Podfile", "platform :ios, '14.0'")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "swift-client-sdk", result.SDKID)
	assert.Equal(t, "cocoapods", result.PackageManager)
}

func TestFileDetector_DetectsSwift_XcodeProj(t *testing.T) {
	dir := t.TempDir()
	// .xcodeproj is a directory in practice, but we use Glob so creating the dir is enough
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "MyApp.xcodeproj"), 0755))

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "swift-client-sdk", result.SDKID)
	assert.Equal(t, "Swift", result.Language)
}

func TestFileDetector_DetectsDotnet_Csproj(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "MyApp.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>")
	writeDetectFile(t, dir, "Program.cs", "// entry")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "dotnet-server-sdk", result.SDKID)
	assert.Equal(t, "C#", result.Language)
	assert.Equal(t, "dotnet", result.PackageManager)
	assert.Equal(t, filepath.Join(dir, "Program.cs"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_DetectsDotnet_Sln(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "MyApp.sln", "")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "dotnet-server-sdk", result.SDKID)
	assert.Equal(t, "dotnet", result.PackageManager)
}

func TestKnownSDKs_ContainsExpectedSDKs(t *testing.T) {
	ids := make([]string, len(KnownSDKs))
	for i, sdk := range KnownSDKs {
		ids[i] = sdk.ID
	}
	assert.Contains(t, ids, "node-server")
	assert.Contains(t, ids, "react-client-sdk")
	assert.Contains(t, ids, "react-native")
	assert.Contains(t, ids, "python-server-sdk")
	assert.Contains(t, ids, "go-server-sdk")
	assert.Contains(t, ids, "java-server-sdk")
	assert.Contains(t, ids, "dotnet-server-sdk")
	assert.Contains(t, ids, "swift-client-sdk")
	assert.Contains(t, ids, "ruby-server-sdk")
}

func TestFileDetector_EntryPointFallback_WhenNoneExist(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0"}}`)
	// No src/App.tsx or other entry point files

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "src/App.tsx"), result.EntryPoint)
	assert.False(t, result.EntryPointExists, "a suggested path must not look like one we found")
}

func TestFileDetector_MalformedPackageJSON_FallsThrough(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `not valid json {{{`)
	// No other project indicators

	_, err := FileDetector{}.Detect(dir)

	// detectNode skips invalid JSON; no other indicators → error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not detect")
}

// A page module may carry 'use client' or be imported by something that does, which
// would ship the server SDK key to the browser, so never target one.
func TestFileDetector_NextJs_AppRouter_SuggestsInstrumentation(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0","next":"^15.0.0"}}`)
	writeDetectFile(t, dir, "app/page.tsx", "export default function Page() {}")
	writeDetectFile(t, dir, "app/layout.tsx", "export default function Layout() {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "node-server", result.SDKID)
	assert.Equal(t, filepath.Join(dir, "instrumentation.ts"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_NextJs_PrefersExistingInstrumentation(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"next":"^15.0.0"}}`)
	writeDetectFile(t, dir, "instrumentation.ts", "export function register() {}")
	writeDetectFile(t, dir, "app/page.tsx", "export default function Page() {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "instrumentation.ts"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_NextJs_PagesRouter_SuggestsInstrumentation(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0","next":"^13.0.0"}}`)
	writeDetectFile(t, dir, "pages/index.tsx", "export default function Home() {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	// pages/* is bundled for the browser, so it is never a server SDK target.
	assert.Equal(t, filepath.Join(dir, "instrumentation.ts"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_NextJs_Empty_SuggestsInstrumentation(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"next":"^15.0.0"}}`)

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "instrumentation.ts"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_Android_FindsKotlinActivityInPackageDir(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle.kts", "plugins { id(\"com.android.application\") }")
	writeDetectFile(t, dir, "app/src/main/AndroidManifest.xml", "<manifest/>")
	writeDetectFile(t, dir, "app/src/main/java/com/example/myapp/MainActivity.kt", "class MainActivity")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "android", result.SDKID)
	assert.Equal(t, filepath.Join(dir, "app/src/main/java/com/example/myapp/MainActivity.kt"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_Android_KotlinSourceRoot(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle.kts", "plugins { id(\"com.android.application\") }")
	writeDetectFile(t, dir, "app/src/main/AndroidManifest.xml", "<manifest/>")
	writeDetectFile(t, dir, "app/src/main/kotlin/com/example/MainActivity.kt", "class MainActivity")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "app/src/main/kotlin/com/example/MainActivity.kt"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_Android_NoAppModule_UsesMatchedSourceRoot(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle", "plugins { id 'com.android.application' }")
	writeDetectFile(t, dir, "src/main/AndroidManifest.xml", "<manifest/>")
	writeDetectFile(t, dir, "src/main/java/com/example/MainActivity.java", "class MainActivity {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	// The old code hardcoded app/src/main/... even for this single-module layout.
	assert.Equal(t, filepath.Join(dir, "src/main/java/com/example/MainActivity.java"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_Android_NoActivity_SuggestsUnderMatchedRoot(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle", "plugins { id 'com.android.application' }")
	writeDetectFile(t, dir, "src/main/AndroidManifest.xml", "<manifest/>")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "src/main/java/MainActivity.kt"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_Java_FindsMainInPackageDir(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "pom.xml", "<project></project>")
	writeDetectFile(t, dir, "src/main/java/com/example/app/Application.java", "class Application {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "java-server-sdk", result.SDKID)
	assert.Equal(t, filepath.Join(dir, "src/main/java/com/example/app/Application.java"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_Ruby_GemfileReportsBundler(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "bundle", result.PackageManager)
}

func TestFileDetector_Ruby_NoGemfileReportsGem(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "mygem.gemspec", "Gem::Specification.new\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "gem", result.PackageManager)
}

func TestFileDetector_PythonPackageManagers(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"pip", map[string]string{"requirements.txt": "flask\n"}, "pip"},
		{"poetry", map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"myapp\"\n"}, "poetry"},
		{"uv lockfile", map[string]string{"pyproject.toml": "[project]\nname = \"myapp\"\n", "uv.lock": "version = 1\n"}, "uv"},
		{"uv section", map[string]string{"pyproject.toml": "[project]\nname = \"a\"\n[tool.uv]\n"}, "uv"},
		{"pipenv", map[string]string{"Pipfile": "[packages]\n"}, "pipenv"},
		{"bare pyproject", map[string]string{"pyproject.toml": "[project]\nname = \"myapp\"\n"}, "pip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeDetectFile(t, dir, name, content)
			}

			result, err := FileDetector{}.Detect(dir)

			require.NoError(t, err)
			assert.Equal(t, "python-server-sdk", result.SDKID)
			assert.Equal(t, tt.want, result.PackageManager)
		})
	}
}

func TestFileDetector_DetectsNodePM_BunBinaryLockfile(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{}`)
	writeDetectFile(t, dir, "bun.lockb", "")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "bun", result.PackageManager)
}

func TestKnownSDKs_UsesAndroidID(t *testing.T) {
	ids := make([]string, len(KnownSDKs))
	for i, sdk := range KnownSDKs {
		ids[i] = sdk.ID
	}
	assert.Contains(t, ids, "android")
	assert.NotContains(t, ids, "android-client-sdk")
}

func TestEntryPoint_NoCandidateExists_ReturnsFallback(t *testing.T) {
	dir := t.TempDir()

	got, exists := entryPoint(dir, "fallback.go", "nonexistent.go", "also-nonexistent.go")

	assert.Equal(t, filepath.Join(dir, "fallback.go"), got)
	assert.False(t, exists)
}

func TestEntryPoint_MatchesFirstExisting(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "second.go", "")
	writeDetectFile(t, dir, "first.go", "")

	got, exists := entryPoint(dir, "fallback.go", "first.go", "second.go")

	assert.Equal(t, filepath.Join(dir, "first.go"), got)
	assert.True(t, exists)
}

func TestEntryPoint_SkipsEmptyAndDirectoryCandidates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0755))
	writeDetectFile(t, dir, "real.go", "")

	got, exists := entryPoint(dir, "fallback.go", "", "src", "real.go")

	assert.Equal(t, filepath.Join(dir, "real.go"), got)
	assert.True(t, exists)
}

func TestFindFileUnder(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "src/main/java/com/example/App.java", "")

	assert.Equal(t, filepath.Join("src/main/java/com/example/App.java"),
		findFileUnder(dir, "src/main/java", "Main.java", "App.java"))
	assert.Empty(t, findFileUnder(dir, "src/main/java", "Missing.java"))
	assert.Empty(t, findFileUnder(dir, "does/not/exist", "App.java"))
}

// Multi-binary repos have no single entry point, so the detector must not pick one
// of them arbitrarily and report it as found.
func TestFileDetector_Go_MultipleBinaries_Suggests(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	writeDetectFile(t, dir, "cmd/server/main.go", "package main\n")
	writeDetectFile(t, dir, "cmd/worker/main.go", "package main\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "main.go"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

// An entry file named after the module, as ld-relay and gonfalon do, is not something
// we can guess at either.
func TestFileDetector_Go_ModuleNamedEntryFile_Suggests(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "go.mod", "module github.com/launchdarkly/ld-relay/v8\n\ngo 1.22\n")
	writeDetectFile(t, dir, "ld-relay.go", "package main\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_Swift_NestedSourcesTarget(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")
	// `swift package init` names the file after the target, not main.swift.
	writeDetectFile(t, dir, "Sources/MyTool/MyTool.swift", "print(1)")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Sources/MyTool/MyTool.swift"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_Swift_PrefersMainSwiftInSources(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")
	writeDetectFile(t, dir, "Sources/MyTool/Helper.swift", "")
	writeDetectFile(t, dir, "Sources/MyTool/main.swift", "print(1)")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Sources/MyTool/main.swift"), result.EntryPoint)
}

func TestFileDetector_Swift_XcodeAppNamedDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "MyApp.xcodeproj"), 0755))
	// Xcode's SwiftUI template puts the app code in a directory named after the project.
	writeDetectFile(t, dir, "MyApp/MyAppApp.swift", "@main struct MyAppApp {}")
	writeDetectFile(t, dir, "MyApp/ContentView.swift", "struct ContentView {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "MyApp/MyAppApp.swift"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFileDetector_Swift_NoSources_Suggests(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "App.swift"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_React_ViteMountPoint(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0"}}`)
	// Vite scaffolds src/main.tsx; without App.tsx the old list fell through to a
	// nonexistent src/App.tsx even though the mount point was right there.
	writeDetectFile(t, dir, "src/main.tsx", "createRoot()")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "src/main.tsx"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestFindFileUnder_SuffixPattern(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "MyApp/MyAppApp.swift", "")

	assert.Equal(t, filepath.Join("MyApp/MyAppApp.swift"), findFileUnder(dir, "MyApp", "*App.swift"))
	assert.Empty(t, findFileUnder(dir, "MyApp", "*.kt"))
}

// An empty root must not walk the whole project.
func TestFindFileUnder_EmptyRoot(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "deep/nested/App.swift", "")

	assert.Empty(t, findFileUnder(dir, "", "App.swift"))
}

// With several targets there is no way to tell an entry point from a helper, so the
// detector must not present an arbitrary pick as found.
func TestFileDetector_Swift_MultipleTargets_Suggests(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")
	writeDetectFile(t, dir, "Sources/Alpha/Helper.swift", "struct Helper {}")
	writeDetectFile(t, dir, "Sources/Beta/Beta.swift", "@main struct Beta {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "App.swift"), result.EntryPoint)
	assert.False(t, result.EntryPointExists)
}

func TestFileDetector_Swift_SingleTarget_PrefersTargetNamedFile(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")
	// Helper.swift sorts first, but MyTool.swift is the entry file.
	writeDetectFile(t, dir, "Sources/MyTool/Helper.swift", "struct Helper {}")
	writeDetectFile(t, dir, "Sources/MyTool/MyTool.swift", "@main struct MyTool {}")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Sources/MyTool/MyTool.swift"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}

func TestSoleSubdir(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "one/Alpha/a.swift", "")
	writeDetectFile(t, dir, "two/Alpha/a.swift", "")
	writeDetectFile(t, dir, "two/Beta/b.swift", "")
	writeDetectFile(t, dir, "files/a.swift", "")

	assert.Equal(t, filepath.Join("one/Alpha"), soleSubdir(dir, "one"))
	assert.Empty(t, soleSubdir(dir, "two"), "two subdirectories is ambiguous")
	assert.Empty(t, soleSubdir(dir, "files"), "files are not targets")
	assert.Empty(t, soleSubdir(dir, "missing"))
}

// A root package.json is often only build tooling, so a backend manifest wins.
func TestFileDetector_Polyglot_BackendManifestWins(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantSDK string
	}{
		{"rails with jsbundling", map[string]string{
			"Gemfile": "source 'https://rubygems.org'\ngem 'rails'\n", "package.json": `{"dependencies":{"esbuild":"0.20.0"}}`,
		}, "ruby-server-sdk"},
		{"django with tailwind", map[string]string{
			"requirements.txt": "Django==5.0\n", "package.json": `{"devDependencies":{"tailwindcss":"3.4.0"}}`,
		}, "python-server-sdk"},
		{"go binary published to npm", map[string]string{
			"go.mod": "module example.com/app\n\ngo 1.22\n", "package.json": `{"name":"app-cli"}`,
		}, "go-server-sdk"},
		{"dotnet with npm assets", map[string]string{
			"App.csproj": "<Project/>", "package.json": `{"devDependencies":{"vite":"5.0.0"}}`,
		}, "dotnet-server-sdk"},
		// package.json is the only manifest, so Node still claims it.
		{"plain next.js", map[string]string{
			"package.json": `{"dependencies":{"next":"15.0.0"}}`,
		}, "node-server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeDetectFile(t, dir, name, content)
			}

			result, err := FileDetector{}.Detect(dir)

			require.NoError(t, err)
			assert.Equal(t, tt.wantSDK, result.SDKID)
		})
	}
}

// Searching Sources/ is confined to single-target packages, so neither main.swift
// nor a *App.swift in one of several targets may be reported as found.
func TestFileDetector_Swift_MultipleTargets_NeverReportsFound(t *testing.T) {
	for _, entry := range []string{"Sources/Beta/main.swift", "Sources/Zeta/ZetaApp.swift", "Sources/Beta/Beta.swift"} {
		t.Run(entry, func(t *testing.T) {
			dir := t.TempDir()
			writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")
			writeDetectFile(t, dir, "Sources/Alpha/Helper.swift", "struct Helper {}")
			writeDetectFile(t, dir, entry, "// entry")

			result, err := FileDetector{}.Detect(dir)

			require.NoError(t, err)
			assert.Equal(t, filepath.Join(dir, "App.swift"), result.EntryPoint)
			assert.False(t, result.EntryPointExists)
		})
	}
}

func TestFileDetector_Swift_SingleTarget_PrefersMainSwift(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Package.swift", "// swift-tools-version:5.9")
	writeDetectFile(t, dir, "Sources/MyTool/Helper.swift", "")
	writeDetectFile(t, dir, "Sources/MyTool/MyTool.swift", "")
	writeDetectFile(t, dir, "Sources/MyTool/main.swift", "print(1)")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "Sources/MyTool/main.swift"), result.EntryPoint)
	assert.True(t, result.EntryPointExists)
}
