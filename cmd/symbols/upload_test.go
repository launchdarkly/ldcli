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
}

func TestGetS3Key(t *testing.T) {
	assert.Equal(t, "1.0.1/index.android.bundle.map", getS3Key("1.0.1", "", "index.android.bundle.map"))
	assert.Equal(t, "unversioned/main.jsbundle.map", getS3Key("", "", "main.jsbundle.map"))
	assert.Equal(t, "1.0.1/dist/main.jsbundle", getS3Key("1.0.1", "dist", "main.jsbundle"))
}

func TestUnsupportedType(t *testing.T) {
	viper.Set(typeFlag, "apple-dsym")
	defer viper.Set(typeFlag, "")

	client := resources.NewClient("")
	err := runE(client)(&cobra.Command{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported --type")
}
