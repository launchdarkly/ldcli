package sourcemaps

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestVerifyApiKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-api-key", r.Header.Get("ApiKey"))

		response := `{"data":{"api_key_to_org_id":"org123"}}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	orgID, err := verifyApiKey("test-api-key", server.URL)
	assert.NoError(t, err)
	assert.Equal(t, "org123", orgID)

	invalidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{"data":{"api_key_to_org_id":"0"}}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer invalidServer.Close()

	_, err = verifyApiKey("invalid-key", invalidServer.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid API key")
}

func TestGetSourceMapUploadUrls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var requestBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&requestBody)
		assert.NoError(t, err)
		assert.Contains(t, requestBody, "query")
		assert.Contains(t, requestBody, "variables")

		variables, ok := requestBody["variables"].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "test-api-key", variables["api_key"])
		assert.NotNil(t, variables["paths"])

		response := `{"data":{"get_source_map_upload_urls":["https://example.com/upload1","https://example.com/upload2"]}}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	paths := []string{"path1", "path2"}
	urls, err := getSourceMapUploadUrls("test-api-key", paths, server.URL)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(urls))
	assert.Equal(t, "https://example.com/upload1", urls[0])
	assert.Equal(t, "https://example.com/upload2", urls[1])

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{"data":{"get_source_map_upload_urls":[]}}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer errorServer.Close()

	_, err = getSourceMapUploadUrls("test-api-key", paths, errorServer.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to generate source map upload urls")
}

func TestGetS3Key(t *testing.T) {
	key := getS3Key("org123", "v1.0", "base/path", "file.js.map")
	assert.Equal(t, "org123/v1.0/base/path/file.js.map", key)

	key = getS3Key("org123", "", "base/path", "file.js.map")
	assert.Equal(t, "org123/unversioned/base/path/file.js.map", key)

	key = getS3Key("org123", "v1.0", "", "file.js.map")
	assert.Equal(t, "org123/v1.0/file.js.map", key)

	key = getS3Key("org123", "v1.0", "base/path", "file.js.map")
	assert.Equal(t, "org123/v1.0/base/path/file.js.map", key)
}

func TestUploadFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sourcemap-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "test.js.map")
	err = os.WriteFile(tempFile, []byte("test content"), 0644)
	assert.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "test content", string(body))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err = uploadFile(tempFile, server.URL, "test.js.map")
	assert.NoError(t, err)

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	err = uploadFile(tempFile, errorServer.URL, "test.js.map")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload failed with status code: 500")
}

func TestNewUploadCmd(t *testing.T) {
	client := resources.NewClient("")
	cmd := NewUploadCmd(client)

	assert.Equal(t, "upload", cmd.Use)
	assert.Equal(t, "Upload sourcemaps", cmd.Short)
	assert.Contains(t, cmd.Long, "LaunchDarkly for error monitoring")

	assert.NotNil(t, cmd.Flags().Lookup(apiKeyFlag))
	assert.NotNil(t, cmd.Flags().Lookup(appVersionFlag))
	assert.NotNil(t, cmd.Flags().Lookup(pathFlag))
	assert.NotNil(t, cmd.Flags().Lookup(basePathFlag))
	assert.NotNil(t, cmd.Flags().Lookup(backendUrlFlag))

	requiredFlags := cmd.Flags().Lookup(apiKeyFlag).Annotations["required"]
	assert.Equal(t, []string{"true"}, requiredFlags)
}

func TestGetAllSourceMapFiles(t *testing.T) {
	singleFile := "/tmp/sourcemap-test-files/test.js.map"
	files, err := getAllSourceMapFiles(singleFile)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(files))
	assert.Equal(t, "test.js.map", files[0].Name)
	assert.Equal(t, singleFile, files[0].Path)

	dirPath := "/tmp/sourcemap-test-files"
	files, err = getAllSourceMapFiles(dirPath)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(files), 3) // At least 3 files (test.js.map, test.js, route.js.map)
	
	for _, file := range files {
		assert.NotContains(t, file.Path, "node_modules")
	}

	var foundRouteGroup bool
	var foundRouteGroupRemoved bool
	for _, file := range files {
		if file.Path == "/tmp/sourcemap-test-files/routes/(group)/nested/route.js.map" {
			foundRouteGroup = true
		}
		if file.Name == "routes/nested/route.js.map" {
			foundRouteGroupRemoved = true
		}
	}
	assert.True(t, foundRouteGroup, "Should find the route group file")
	assert.True(t, foundRouteGroupRemoved, "Should find the route group file with group removed")

	_, err = getAllSourceMapFiles("/non-existent-path")
	assert.Error(t, err)

	emptyDir, err := os.MkdirTemp("", "empty-dir")
	assert.NoError(t, err)
	defer os.RemoveAll(emptyDir)
	_, err = getAllSourceMapFiles(emptyDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no .js.map files found")
}

func TestVerifyApiKeyErrors(t *testing.T) {
	_, err := verifyApiKey("test-key", "://invalid-url")
	assert.Error(t, err)

	_, err = verifyApiKey("test-key", "http://non-existent-host.invalid")
	assert.Error(t, err)

	invalidJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":invalid-json`))
	}))
	defer invalidJSONServer.Close()

	_, err = verifyApiKey("test-key", invalidJSONServer.URL)
	assert.Error(t, err)
}

func TestGetSourceMapUploadUrlsErrors(t *testing.T) {
	_, err := getSourceMapUploadUrls("test-key", []string{"path"}, "://invalid-url")
	assert.Error(t, err)

	_, err = getSourceMapUploadUrls("test-key", []string{"path"}, "http://non-existent-host.invalid")
	assert.Error(t, err)

	invalidJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":invalid-json`))
	}))
	defer invalidJSONServer.Close()

	_, err = getSourceMapUploadUrls("test-key", []string{"path"}, invalidJSONServer.URL)
	assert.Error(t, err)
}

func TestRunE(t *testing.T) {
	client := resources.NewClient("")

	cmd := NewUploadCmd(client)
	args := []string{}

	tempDir, err := os.MkdirTemp("", "sourcemap-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	testMapFile := filepath.Join(tempDir, "test.js.map")
	err = os.WriteFile(testMapFile, []byte("{}"), 0644)
	assert.NoError(t, err)

	verifyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{"data":{"api_key_to_org_id":"org123"}}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer verifyServer.Close()

	urlsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := `{"data":{"get_source_map_upload_urls":["https://example.com/upload"]}}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer urlsServer.Close()

	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	runFunc := runE(client)
	err = runFunc(cmd, args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api key cannot be empty")

	os.Setenv("HIGHLIGHT_SOURCEMAP_UPLOAD_API_KEY", "test-api-key")
	defer os.Unsetenv("HIGHLIGHT_SOURCEMAP_UPLOAD_API_KEY")
	
	cmd.Flags().Set(pathFlag, testMapFile)
	cmd.Flags().Set(backendUrlFlag, verifyServer.URL)
	
	err = runFunc(cmd, args)
	assert.Error(t, err)
}
