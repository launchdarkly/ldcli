package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorsHeadersWithConfig_Enabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("test response"))
		require.NoError(t, err)
	})

	corsHandler := CorsHeadersWithConfig(true, "*")(handler)

	// Test GET request
	req := httptest.NewRequest("GET", "/dev/projects", nil)
	w := httptest.NewRecorder()
	corsHandler.ServeHTTP(w, req)

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET,POST,PUT,PATCH,DELETE,OPTIONS", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCorsHeadersWithConfig_OptionsRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for OPTIONS request")
	})

	corsHandler := CorsHeadersWithConfig(true, "https://example.com")(handler)

	// Test OPTIONS preflight request
	req := httptest.NewRequest("OPTIONS", "/dev/projects", nil)
	w := httptest.NewRecorder()
	corsHandler.ServeHTTP(w, req)

	assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCorsHeadersWithConfig_Disabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("test response"))
		require.NoError(t, err)
	})

	corsHandler := CorsHeadersWithConfig(false, "*")(handler)

	// Test GET request with CORS disabled
	req := httptest.NewRequest("GET", "/dev/projects", nil)
	w := httptest.NewRecorder()
	corsHandler.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"), "Expected no CORS headers when disabled")
	assert.Equal(t, http.StatusOK, w.Code)
}
