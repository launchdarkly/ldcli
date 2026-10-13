package symbols

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeZip builds an APK/AAB stand-in: both are zips, and only one entry matters
// here.
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	file, err := os.Create(path)
	require.NoError(t, err)
	defer file.Close()

	archive := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := archive.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, archive.Close())
}

// writeAndroidApk packages an app beside the metadata that names it, which is how
// the APK for a variant is found.
func writeAndroidApk(t *testing.T, root, module string, entries map[string]string, apkDirs ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{root, module, "build", "outputs", "apk"}, append(apkDirs, "app.apk")...)...)
	writeZip(t, path, entries)
	return path
}

const stampedSymbolsID = "7e0d66142a85de6c6b2850dcbba5f066"

func TestSymbolsIDFromPackagedAPK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.apk")
	writeZip(t, path, map[string]string{
		"AndroidManifest.xml": "binary",
		apkSymbolsIDEntry:     stampedSymbolsID + "\n",
	})

	assert.Equal(t, stampedSymbolsID, symbolsIDFromPackagedApp(path))
}

func TestSymbolsIDFromAppBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.aab")
	writeZip(t, path, map[string]string{aabSymbolsIDEntry: stampedSymbolsID})

	assert.Equal(t, stampedSymbolsID, symbolsIDFromPackagedApp(path))
}

// An app that does not stamp an id has nothing to be keyed by, and belongs on the
// Version Lane.
func TestSymbolsIDFromPackagedAppWithoutAsset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.apk")
	writeZip(t, path, map[string]string{"AndroidManifest.xml": "binary"})

	assert.Empty(t, symbolsIDFromPackagedApp(path))
}

// The SDK reports only a 32-hex-char id, so anything else names a lane that will
// never be asked for — and an id is part of a storage key, which is not somewhere
// arbitrary text from a file belongs.
func TestSymbolsIDFromPackagedAppRejectsMalformedID(t *testing.T) {
	for _, id := range []string{
		"",
		"   ",
		"not-a-symbols-id",
		"7E0D66142A85DE6C6B2850DCBBA5F066",
		"7e0d66142a85de6c6b2850dcbba5f0",
		"../../../../etc/passwd",
	} {
		path := filepath.Join(t.TempDir(), "app.apk")
		writeZip(t, path, map[string]string{apkSymbolsIDEntry: id})
		assert.Empty(t, symbolsIDFromPackagedApp(path), "expected rejected: %q", id)
	}
}

func TestSymbolsIDFromPackagedAppUnreadable(t *testing.T) {
	dir := t.TempDir()
	notAZip := filepath.Join(dir, "app.apk")
	require.NoError(t, os.WriteFile(notAZip, []byte("this is not a zip"), 0o644))

	assert.Empty(t, symbolsIDFromPackagedApp(notAZip))
	assert.Empty(t, symbolsIDFromPackagedApp(filepath.Join(dir, "missing.apk")))
}

func TestAndroidBuildSymbolsID(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")
	writeAndroidApk(t, root, "app", map[string]string{apkSymbolsIDEntry: stampedSymbolsID}, "compose", "release")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Equal(t, stampedSymbolsID, build.SymbolsID())
}

// Another variant's APK describes another build, whose id would send this mapping
// to a lane that build's app already occupies.
func TestAndroidBuildSymbolsIDIgnoresOtherVariants(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "javaRelease", "1.0.1", "java", "release")
	writeAndroidApk(t, root, "app", map[string]string{apkSymbolsIDEntry: stampedSymbolsID}, "java", "release")

	build, err := discoverAndroidBuild(root)
	require.NoError(t, err)
	require.NotNil(t, build)
	assert.Empty(t, build.SymbolsID())
}

func TestResolveAndroidBuildUsesStampedSymbolsID(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")
	writeAndroidApk(t, root, "app", map[string]string{apkSymbolsIDEntry: stampedSymbolsID}, "compose", "release")

	resolved, err := resolveAndroidBuild(androidUpload{Path: root})
	require.NoError(t, err)
	assert.Equal(t, stampedSymbolsID, resolved.SymbolsID)

	// The id fully addresses the mapping, so it supersedes the version.
	assert.Equal(t,
		"_sym/android/id/"+stampedSymbolsID+"/mapping.txt",
		getS3Key(androidSymbolsIDPrefix, resolved.SymbolsID, resolved.AppVersion, "", androidMappingFileName),
	)
}

func TestResolveAndroidBuildKeepsExplicitSymbolsID(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")
	writeAndroidApk(t, root, "app", map[string]string{apkSymbolsIDEntry: stampedSymbolsID}, "compose", "release")

	const explicit = "0123456789abcdef0123456789abcdef"
	resolved, err := resolveAndroidBuild(androidUpload{Path: root, SymbolsID: explicit})
	require.NoError(t, err)
	assert.Equal(t, explicit, resolved.SymbolsID)
}

// A mapping with no packaged app beside it still uploads, on the Version Lane.
func TestResolveAndroidBuildWithoutPackagedApp(t *testing.T) {
	root := t.TempDir()
	writeAndroidMapping(t, root, "app", "composeRelease", "com.example.App -> a.a:\n")
	writeAndroidOutputMetadata(t, root, "app", "composeRelease", "1.0.1", "compose", "release")

	resolved, err := resolveAndroidBuild(androidUpload{Path: root})
	require.NoError(t, err)
	assert.Empty(t, resolved.SymbolsID)
	assert.Equal(t, "1.0.1", resolved.AppVersion)
}
