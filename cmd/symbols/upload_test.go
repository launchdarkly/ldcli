package symbols

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestNewUploadCmd(t *testing.T) {
	client := resources.NewClient("")
	cmd := NewUploadCmd(client, func(accessToken, baseURI string, analyticsOptOut bool) analytics.Tracker {
		return &analytics.MockTracker{}
	})

	assert.Equal(t, "upload", cmd.Use)
	assert.Equal(t, "Upload symbol files", cmd.Short)

	assert.NotNil(t, cmd.Flags().Lookup(typeFlag))
	assert.NotNil(t, cmd.Flags().Lookup("project"))
	assert.NotNil(t, cmd.Flags().Lookup(appVersionFlag))
	assert.NotNil(t, cmd.Flags().Lookup(symbolsIdFlag))
	assert.NotNil(t, cmd.Flags().Lookup(pathFlag))
	assert.NotNil(t, cmd.Flags().Lookup(basePathFlag))
	assert.NotNil(t, cmd.Flags().Lookup(backendUrlFlag))

	assert.Equal(t, []string{"true"}, cmd.Flags().Lookup(typeFlag).Annotations["required"])
	assert.Equal(t, []string{"true"}, cmd.Flags().Lookup("project").Annotations["required"])
}

func TestIsReactNativeUploadFile(t *testing.T) {
	// React Native iOS bundle + map.
	assert.True(t, isReactNativeUploadFile("main.jsbundle"))
	assert.True(t, isReactNativeUploadFile("main.jsbundle.map"))
	// React Native Android bundle + map.
	assert.True(t, isReactNativeUploadFile("index.android.bundle"))
	assert.True(t, isReactNativeUploadFile("index.android.bundle.map"))
	// Web bundles are handled by `sourcemaps upload`, not here.
	assert.False(t, isReactNativeUploadFile("app.js"))
	assert.False(t, isReactNativeUploadFile("app.js.map"))
	// Unrelated files are ignored.
	assert.False(t, isReactNativeUploadFile("assets.png"))
	assert.False(t, isReactNativeUploadFile("README.md"))
}

func TestGetAllSymbolFilesReactNative(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "symbols-rn-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	rnFiles := []string{
		"main.jsbundle",
		"main.jsbundle.map",
		"index.android.bundle",
		"index.android.bundle.map",
	}
	for _, name := range rnFiles {
		err = os.WriteFile(filepath.Join(tempDir, name), []byte("{}"), 0644)
		assert.NoError(t, err)
	}
	// Non-symbol files that must be skipped.
	err = os.WriteFile(filepath.Join(tempDir, "assets.png"), []byte("x"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(filepath.Join(tempDir, "app.js"), []byte("x"), 0644)
	assert.NoError(t, err)

	files, err := getAllSymbolFiles(tempDir, typeReactNative)
	assert.NoError(t, err)

	found := make(map[string]bool)
	for _, f := range files {
		found[f.Name] = true
	}
	for _, name := range rnFiles {
		assert.True(t, found[name], "expected %s to be discovered for upload", name)
	}
	assert.False(t, found["assets.png"], "non-symbol files must be skipped")
	assert.False(t, found["app.js"], "web bundles must be skipped (handled by sourcemaps upload)")
}

func TestGetAllSymbolFilesEmpty(t *testing.T) {
	emptyDir, err := os.MkdirTemp("", "symbols-empty")
	assert.NoError(t, err)
	defer os.RemoveAll(emptyDir)

	_, err = getAllSymbolFiles(emptyDir, typeReactNative)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no React Native symbol files found")

	_, err = getAllSymbolFiles(emptyDir, typeAndroid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no Android symbol files found")
}

func TestGetAllSymbolFilesSingleFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "symbols-single")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	validMap := filepath.Join(tempDir, "main.jsbundle.map")
	assert.NoError(t, os.WriteFile(validMap, []byte("{}"), 0644))
	unrelated := filepath.Join(tempDir, "secrets.txt")
	assert.NoError(t, os.WriteFile(unrelated, []byte("nope"), 0644))
	mapping := filepath.Join(tempDir, androidMappingFileName)
	assert.NoError(t, os.WriteFile(mapping, []byte("a -> b:\n"), 0644))

	// A matching file for the type is accepted.
	files, err := getAllSymbolFiles(validMap, typeReactNative)
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, "main.jsbundle.map", files[0].Name)

	// An unrelated single file is rejected instead of uploaded under symbol keys.
	_, err = getAllSymbolFiles(unrelated, typeReactNative)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is not a React Native symbol file")

	// A file valid for one type is rejected when the wrong type is chosen.
	_, err = getAllSymbolFiles(validMap, typeAndroid)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is not an Android symbol file")

	_, err = getAllSymbolFiles(mapping, typeAndroid)
	assert.NoError(t, err)
}

func TestIsSymbolUploadFileAndroid(t *testing.T) {
	// Only mapping.txt is uploaded for the android type.
	assert.True(t, isSymbolUploadFile(typeAndroid, "mapping.txt"))
	assert.True(t, isSymbolUploadFile(typeAndroid, "outputs/mapping/release/mapping.txt"))
	assert.False(t, isSymbolUploadFile(typeAndroid, "seeds.txt"))
	assert.False(t, isSymbolUploadFile(typeAndroid, "main.jsbundle.map"))
	// React Native discovery is unaffected.
	assert.True(t, isSymbolUploadFile(typeReactNative, "index.android.bundle.map"))
	assert.False(t, isSymbolUploadFile(typeReactNative, "mapping.txt"))
}

func TestGetAllSymbolFilesAndroid(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "symbols-android-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = os.WriteFile(filepath.Join(tempDir, androidMappingFileName), []byte("com.example.Foo -> a:\n"), 0644)
	assert.NoError(t, err)
	// A React Native map alongside must NOT be picked up for the android type.
	err = os.WriteFile(filepath.Join(tempDir, "main.jsbundle.map"), []byte("{}"), 0644)
	assert.NoError(t, err)

	files, err := getAllSymbolFiles(tempDir, typeAndroid)
	assert.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Equal(t, androidMappingFileName, files[0].Name)
}

func TestGetS3Key(t *testing.T) {
	// Version Lane (version/basePath) addressing; prefix is unused when symbolsID is "".
	assert.Equal(t, "1.0.1/index.android.bundle.map", getS3Key(reactNativeSymbolsIDPrefix, "", "1.0.1", "", "index.android.bundle.map"))
	assert.Equal(t, "unversioned/main.jsbundle.map", getS3Key(reactNativeSymbolsIDPrefix, "", "", "", "main.jsbundle.map"))
	assert.Equal(t, "1.0.1/dist/main.jsbundle", getS3Key(reactNativeSymbolsIDPrefix, "", "1.0.1", "dist", "main.jsbundle"))
	assert.Equal(t, "1.0.1/mapping.txt", getS3Key(androidSymbolsIDPrefix, "", "1.0.1", "", "mapping.txt"))
}

func TestGetS3KeySymbolsID(t *testing.T) {
	symbolsID := "0123456789abcdef0123456789abcdef"
	// Symbols Id Lane: a symbols id supersedes version/basePath and keys by basename.
	assert.Equal(t,
		"_sym/js/id/"+symbolsID+"/main.jsbundle.map",
		getS3Key(reactNativeSymbolsIDPrefix, symbolsID, "1.0.1", "dist", "main.jsbundle.map"))
	assert.Equal(t,
		"_sym/js/id/"+symbolsID+"/index.android.bundle.map",
		getS3Key(reactNativeSymbolsIDPrefix, symbolsID, "", "", "nested/index.android.bundle.map"))
	// Android uses its own Symbols Id Lane namespace so JS and mapping ids never collide.
	assert.Equal(t,
		"_sym/android/id/"+symbolsID+"/mapping.txt",
		getS3Key(androidSymbolsIDPrefix, symbolsID, "1.0.0", "", "mapping.txt"))
}

func TestSymbolsIDPrefixForType(t *testing.T) {
	assert.Equal(t, "_sym/js/id", symbolsIDPrefixForType(typeReactNative))
	assert.Equal(t, "_sym/android/id", symbolsIDPrefixForType(typeAndroid))
}

func TestReadSymbolsIDFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "symbols-id")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	map1 := filepath.Join(tempDir, "main.jsbundle.map")
	map2 := filepath.Join(tempDir, "index.android.bundle.map")

	// No sidecar yet -> empty (falls back to the Version Lane).
	assert.Equal(t, "", readSymbolsIDFile(map1+symbolsIDSidecarSuffix))

	err = os.WriteFile(map1+symbolsIDSidecarSuffix, []byte("iosid\n"), 0644)
	assert.NoError(t, err)
	err = os.WriteFile(map2+symbolsIDSidecarSuffix, []byte("androidid\n"), 0644)
	assert.NoError(t, err)

	// Each artifact resolves its own adjacent sidecar so a mixed-platform dir
	// keys each map by the id its app reports (not the first one found).
	assert.Equal(t, "iosid", readSymbolsIDFile(map1+symbolsIDSidecarSuffix))
	assert.Equal(t, "androidid", readSymbolsIDFile(map2+symbolsIDSidecarSuffix))
	assert.Equal(t,
		"_sym/js/id/iosid/main.jsbundle.map",
		getS3Key(reactNativeSymbolsIDPrefix, "iosid", "1.0.0", "", "main.jsbundle.map"))
	assert.Equal(t,
		"_sym/js/id/androidid/index.android.bundle.map",
		getS3Key(reactNativeSymbolsIDPrefix, "androidid", "1.0.0", "", "index.android.bundle.map"))
}

func TestSymbolsIDForArtifact(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "symbols-id-sibling")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	bundle := filepath.Join(tempDir, "main.jsbundle")
	bundleMap := bundle + ".map"

	// The Metro plugin writes a single sidecar named after the source map.
	err = os.WriteFile(bundleMap+symbolsIDSidecarSuffix, []byte("sharedid\n"), 0644)
	assert.NoError(t, err)

	// Both the bundle and its .map must resolve to the same id, even though only
	// the map has an adjacent sidecar — otherwise the bundle would drop to the
	// Version Lane while the map stays on the Symbols Id Lane.
	assert.Equal(t, "sharedid", symbolsIDForArtifact(bundleMap))
	assert.Equal(t, "sharedid", symbolsIDForArtifact(bundle))

	// Symmetric case: sidecar written beside the bundle instead of the map.
	tempDir2, err := os.MkdirTemp("", "symbols-id-sibling2")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir2)

	bundle2 := filepath.Join(tempDir2, "index.android.bundle")
	err = os.WriteFile(bundle2+symbolsIDSidecarSuffix, []byte("bundleid\n"), 0644)
	assert.NoError(t, err)
	assert.Equal(t, "bundleid", symbolsIDForArtifact(bundle2))
	assert.Equal(t, "bundleid", symbolsIDForArtifact(bundle2+".map"))

	// No sidecar anywhere -> empty (Version Lane fallback).
	assert.Equal(t, "", symbolsIDForArtifact(filepath.Join(tempDir2, "other.jsbundle")))
}

func TestUnsupportedType(t *testing.T) {
	viper.Set(typeFlag, "flutter")
	defer viper.Set(typeFlag, "")

	client := resources.NewClient("")
	err := runE(client)(&cobra.Command{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported --type")
}

func TestIsSupportedType(t *testing.T) {
	assert.True(t, isSupportedType(typeReactNative))
	assert.True(t, isSupportedType(typeAndroid))
	assert.True(t, isSupportedType(typeAppleDSYM))
	assert.False(t, isSupportedType("flutter"))
	assert.False(t, isSupportedType(""))
}
