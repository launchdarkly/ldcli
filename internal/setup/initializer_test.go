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
		{"android-client-sdk", "android-client-sdk", "mob-key-789"},
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
	assert.True(t, HasTemplate("android-client-sdk"))
	assert.False(t, HasTemplate("android"))
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

func TestInjectIntoFile_NewFile_OmitsSeparator(t *testing.T) {
	sdks := []struct {
		sdkID    string
		filename string
	}{
		{"python-server-sdk", "init_ld.py"},
		{"ruby-server-sdk", "init_ld.rb"},
		{"node-server", "index.js"},
	}

	for _, tt := range sdks {
		t.Run(tt.sdkID, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, tt.filename)

			initializer := Initializer{}
			result, err := initializer.InjectIntoFile(tt.sdkID, filePath, InitConfig{
				SDKKey:  "test-key",
				FlagKey: "test-flag",
			})

			require.NoError(t, err)
			assert.True(t, result.Success)

			content, err := os.ReadFile(filePath)
			require.NoError(t, err)
			assert.NotContains(t, string(content), "// --- init ---")
		})
	}
}

func TestInjectIntoFile_AndroidClientSdk_ReturnsGuidance(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "MainActivity.java")

	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("android-client-sdk", filePath, InitConfig{
		MobileKey: "mob-test-key",
		FlagKey:   "test-flag",
	})

	require.NoError(t, err)
	// Android is a scoped language: statements can't live at file scope, so we
	// return guidance rather than write a broken file.
	assert.False(t, result.Success)
	assert.Equal(t, "android-client-sdk", result.SDKID)
	assert.Contains(t, result.Snippet, "mob-test-key")
	assert.NotEmpty(t, result.DocsURL)

	// The file must not have been created.
	_, statErr := os.Stat(filePath)
	assert.True(t, os.IsNotExist(statErr), "guidance-only SDK must not create the file")
}

func TestInjectIntoFile_Go_ReturnsGuidanceDoesNotModifyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")

	existing := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	err := os.WriteFile(filePath, []byte(existing), 0644)
	require.NoError(t, err)

	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("go-server-sdk", filePath, InitConfig{
		SDKKey:  "sdk-test-key",
		FlagKey: "test-flag",
	})

	require.NoError(t, err)
	// Go statements are illegal at file scope, so appending would not compile.
	// We return the snippet as guidance and leave the file untouched.
	assert.False(t, result.Success)
	assert.Contains(t, result.Snippet, "sdk-test-key")
	assert.Contains(t, result.Snippet, "github.com/launchdarkly/go-server-sdk/v7")

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, existing, string(content), "existing file must not be modified")
}

func TestInjectIntoFile_React_ReturnsGuidanceDoesNotModifyFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "App.tsx")

	existing := "export default function App() { return null }\n"
	err := os.WriteFile(filePath, []byte(existing), 0644)
	require.NoError(t, err)

	initializer := Initializer{}
	result, err := initializer.InjectIntoFile("react-client-sdk", filePath, InitConfig{
		ClientSideID: "client-id-456",
		FlagKey:      "test-flag",
	})

	require.NoError(t, err)
	// React init must be wired into the component tree, not appended, so we
	// return guidance rather than corrupt the file.
	assert.False(t, result.Success)
	assert.Contains(t, result.Snippet, "asyncWithLDProvider")

	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, existing, string(content), "existing file must not be modified")
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
