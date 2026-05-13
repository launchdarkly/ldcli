package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStubDetector_ReturnsError(t *testing.T) {
	d := StubDetector{}
	result, err := d.Detect("/tmp")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestStubInstaller_ReturnsError(t *testing.T) {
	i := StubInstaller{}
	result, err := i.Install("/tmp", &DetectResult{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not yet implemented")
}

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
	assert.Equal(t, "android-client-sdk", result.SDKID)
	assert.Equal(t, "Java", result.Language)
	assert.Equal(t, "gradle", result.PackageManager)
}

func TestFileDetector_DetectsAndroid_KotlinDsl(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "build.gradle.kts", "plugins { id(\"com.android.application\") }")
	writeDetectFile(t, dir, "app/src/main/AndroidManifest.xml", "<manifest/>")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "android-client-sdk", result.SDKID)
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
	// Falls back to last candidate
	assert.NotEmpty(t, result.EntryPoint)
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

func TestFirstExistingIn_EmptySlice_ReturnsEmpty(t *testing.T) {
	result := firstExistingIn(t.TempDir(), []string{})
	assert.Empty(t, result)
}

func TestFirstExistingIn_NoMatch_ReturnLastCandidate(t *testing.T) {
	dir := t.TempDir()
	result := firstExistingIn(dir, []string{"nonexistent.go", "also-nonexistent.go"})
	assert.Equal(t, "also-nonexistent.go", result)
}

func TestFirstExistingIn_MatchesFirst(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "second.go", "")
	writeDetectFile(t, dir, "first.go", "")
	result := firstExistingIn(dir, []string{"first.go", "second.go"})
	assert.Equal(t, "first.go", result)
}
