package symbols

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAndroidMapping lays out the AGP output an obfuscated build leaves behind:
// module/build/outputs/mapping/<variant>/mapping.txt.
func writeAndroidMapping(t *testing.T, root, module, variant, content string) string {
	t.Helper()
	dir := filepath.Join(root, module, "build", "outputs", "mapping", variant)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, androidMappingFileName)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// writeAndroidOutputMetadata lays out what AGP records when it packages the app,
// under the flavor/buildType directories the APK is written to.
func writeAndroidOutputMetadata(t *testing.T, root, module, variant, versionName string, apkDirs ...string) {
	t.Helper()
	dir := filepath.Join(append([]string{root, module, "build", "outputs", "apk"}, apkDirs...)...)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	metadata := `{
	  "version": 3,
	  "artifactType": {"type": "APK", "kind": "Directory"},
	  "applicationId": "com.example.app",
	  "variantName": "` + variant + `",
	  "elements": [{"type": "SINGLE", "versionCode": 7, "versionName": "` + versionName + `", "outputFile": "app.apk"}]
	}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, androidOutputMetadataName), []byte(metadata), 0o644))
}

func TestDiscoverAndroidBuildFromProjectRoot(t *testing.T) {
	root := t.TempDir()
	mapping := writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, mapping, build.MappingPath)
	assert.Equal(t, "composeRelease", build.Variant)
	assert.Equal(t, filepath.Join(root, "app", "build"), build.BuildDir)
}

// Someone in the module directory rather than the project root is pointing at the
// same build, and gets the same answer.
func TestDiscoverAndroidBuildFromModuleDirectory(t *testing.T) {
	root := t.TempDir()
	mapping := writeAndroidMapping(t, root, "app", "release", "com.example.App -> a.a:\n")

	build, err := discoverAndroidBuild(filepath.Join(root, "app"))
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, mapping, build.MappingPath)
	assert.Equal(t, "release", build.Variant)
}

func TestDiscoverAndroidBuildFromNestedModule(t *testing.T) {
	root := t.TempDir()
	mapping := writeAndroidMapping(t, root, filepath.Join("features", "checkout"), "release", "com.example.App -> a.a:\n")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, mapping, build.MappingPath)
}

// Each variant is a different app, and uploading the wrong one symbolicates every
// frame into the wrong place, so this asks rather than picks.
func TestDiscoverAndroidBuildRefusesToGuessBetweenVariants(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidMapping(t, root, "app", "javaRelease", "com.example.App -> a.a:\n")

	_, err := discoverAndroidBuild(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "composeRelease")
	assert.Contains(t, err.Error(), "javaRelease")
	assert.Contains(t, err.Error(), "--"+pathFlag)
}

// R8 writes an empty mapping for a variant it did not obfuscate. There is nothing
// to retrace with, so it is not a build worth finding — and finding it would make
// a real variant beside it look ambiguous.
func TestDiscoverAndroidBuildIgnoresEmptyMapping(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "debug", "")
	mapping := writeAndroidMapping(t, root, "app", "release", "com.example.App -> a.a:\n")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, mapping, build.MappingPath)
}

// A mapping.txt that is not in an AGP output tree — one staged by a build script,
// say — is left to the ordinary search rather than claimed here.
func TestDiscoverAndroidBuildIgnoresUnconventionalLayout(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "app", "build", "symbols", "composeRelease")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, androidMappingFileName), []byte("x -> a:\n"), 0o644))

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	assert.Nil(t, build)
}

func TestAndroidBuildAppVersion(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, "1.0.1", build.AppVersion())
}

// Without product flavors the APK lands one directory shallower.
func TestAndroidBuildAppVersionWithoutFlavors(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "release", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "release", "2.3.4", "release")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, "2.3.4", build.AppVersion())
}

// One outputs tree holds every variant the module has ever packaged. Taking a
// version from the wrong one would key these symbols to a build they do not describe.
func TestAndroidBuildAppVersionIgnoresOtherVariants(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "javaRelease", "9.9.9", "java", "release")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Empty(t, build.AppVersion())
}

// A mapping with no packaged app beside it still uploads; it just has no version
// to be keyed by.
func TestAndroidBuildAppVersionMissing(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Empty(t, build.AppVersion())
}

func TestResolveAndroidBuildFillsInPathAndVersion(t *testing.T) {
	root := t.TempDir()
	mapping := writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")

	resolved, err := resolveAndroidBuild(androidUpload{Path: root})
	require.NoError(t, err)
	assert.Equal(t, mapping, resolved.Path)
	assert.Equal(t, "1.0.1", resolved.AppVersion)
}

// What the caller asked for wins: an explicit version is the one the app reports,
// whatever the build was packaged as.
func TestResolveAndroidBuildKeepsExplicitVersion(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")

	resolved, err := resolveAndroidBuild(androidUpload{Path: root, AppVersion: "2.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", resolved.AppVersion)
}

// An explicit --path names the mapping, so there is nothing to discover.
func TestResolveAndroidBuildLeavesExplicitFileAlone(t *testing.T) {
	root := t.TempDir()
	mapping := writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")

	resolved, err := resolveAndroidBuild(androidUpload{Path: mapping})
	require.NoError(t, err)
	assert.Equal(t, mapping, resolved.Path)
	assert.Empty(t, resolved.AppVersion)
}

// The mapping is stored as mapping.txt however deep it was found, because that is
// where symbolication reads it.
func TestGetAllSymbolFilesAndroidFlattensName(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")

	files, err := getAllSymbolFiles(root, typeAndroid)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, androidMappingFileName, files[0].Name)
	assert.Equal(t, "1.2.3/mapping.txt", getS3Key(androidSymbolsIDPrefix, "", "1.2.3", "", files[0].Name))
}
