package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	cfg := InitConfig{
		SDKKey:       "sdk-test-key-123",
		ClientSideID: "client-id-456",
		MobileKey:    "mob-key-789",
		FlagKey:      "my-test-flag",
	}

	tests := []struct {
		name       string
		sdkID      string
		wantSubstr string
	}{
		{"node-server", "node-server", "sdk-test-key-123"},
		{"react-client-sdk", "react-client-sdk", "client-id-456"},
		{"react-native", "react-native", "mob-key-789"},
		{"js-client-sdk", "js-client-sdk", "my-test-flag"},
		{"swift-client-sdk", "swift-client-sdk", "mob-key-789"},
		{"android", "android", "mob-key-789"},
		{"java-server-sdk", "java-server-sdk", "sdk-test-key-123"},
		{"ruby-server-sdk", "ruby-server-sdk", "sdk-test-key-123"},
		{"go-server-sdk", "go-server-sdk", "sdk-test-key-123"},
		{"python-server-sdk", "python-server-sdk", "sdk-test-key-123"},
		{"dotnet-server-sdk", "dotnet-server-sdk", "sdk-test-key-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderTemplate(tt.sdkID, cfg)
			require.NoError(t, err)
			assert.Contains(t, result, tt.wantSubstr)
		})
	}
}

func TestRenderTemplateUnknownSDK(t *testing.T) {
	_, err := RenderTemplate("nonexistent-sdk", InitConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no initialization template")
	assert.Contains(t, err.Error(), "see docs")
}

func TestRenderTemplateUnknownSDK_KnownDocsPath(t *testing.T) {
	_, err := RenderTemplate("php-server-sdk", InitConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://launchdarkly.com/docs/sdk/server-side/php")
}

func TestHasTemplate(t *testing.T) {
	assert.True(t, HasTemplate("node-server"))
	assert.True(t, HasTemplate("react-client-sdk"))
	assert.False(t, HasTemplate("nonexistent-sdk"))
}

func TestSupportedSDKIDs(t *testing.T) {
	ids := SupportedSDKIDs()
	assert.Len(t, ids, 11)
	assert.Contains(t, ids, "node-server")
	assert.Contains(t, ids, "react-client-sdk")
	assert.Contains(t, ids, "go-server-sdk")
}

func TestInjectIntoFile_NewFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "index.js")

	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("node-server", filePath, InitConfig{
		SDKKey:  "test-key",
		FlagKey: "test-flag",
	})

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "node-server", result.SDKID)

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test-key")
	assert.Contains(t, string(content), "test-flag")
}

func TestInjectIntoFile_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "app.js")

	err := os.WriteFile(filePath, []byte("// existing code\nconsole.log('hello');\n"), 0644)
	require.NoError(t, err)

	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("node-server", filePath, InitConfig{
		SDKKey:  "test-key",
		FlagKey: "test-flag",
	})

	require.NoError(t, err)
	assert.True(t, result.Success)

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "existing code")
	assert.Contains(t, string(content), "test-key")
}

func TestInjectIntoFile_UnsupportedSDK_ReturnsDocsURL(t *testing.T) {
	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("php-server-sdk", "/tmp/fake.php", InitConfig{})
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Equal(t, "https://launchdarkly.com/docs/sdk/server-side/php", result.DocsURL)
}

func TestInjectIntoFile_CompletelyUnknownSDK_ReturnsFallbackDocsURL(t *testing.T) {
	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("nonexistent-sdk", "/tmp/fake.txt", InitConfig{})
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Equal(t, "https://launchdarkly.com/docs/sdk", result.DocsURL)
}

func TestGetDocsURL(t *testing.T) {
	assert.Equal(t, "https://launchdarkly.com/docs/sdk/server-side/go", GetDocsURL("go-server-sdk"))
	assert.Equal(t, "https://launchdarkly.com/docs/sdk/client-side/react", GetDocsURL("react-client-sdk"))
	assert.Equal(t, "https://launchdarkly.com/docs/sdk/server-side/python", GetDocsURL("python-server-sdk"))
	assert.Equal(t, "https://launchdarkly.com/docs/sdk", GetDocsURL("totally-unknown"))
}
