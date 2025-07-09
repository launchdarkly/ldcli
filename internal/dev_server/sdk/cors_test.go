package sdk

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApiCorsHeadersWithConfig_Enabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	corsHandler := ApiCorsHeadersWithConfig(true, "*")(handler)

	// Test GET request
	req := httptest.NewRequest("GET", "/dev/projects", nil)
	w := httptest.NewRecorder()
	corsHandler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin to be '*', got '%s'", w.Header().Get("Access-Control-Allow-Origin"))
	}

	if w.Header().Get("Access-Control-Allow-Methods") != "GET,POST,PUT,PATCH,DELETE,OPTIONS" {
		t.Errorf("Expected Access-Control-Allow-Methods to include all methods, got '%s'", w.Header().Get("Access-Control-Allow-Methods"))
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", w.Code)
	}
}

func TestApiCorsHeadersWithConfig_OptionsRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for OPTIONS request")
	})

	corsHandler := ApiCorsHeadersWithConfig(true, "https://example.com")(handler)

	// Test OPTIONS preflight request
	req := httptest.NewRequest("OPTIONS", "/dev/projects", nil)
	w := httptest.NewRecorder()
	corsHandler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin to be 'https://example.com', got '%s'", w.Header().Get("Access-Control-Allow-Origin"))
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200 for OPTIONS request, got %d", w.Code)
	}
}

func TestApiCorsHeadersWithConfig_Disabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	corsHandler := ApiCorsHeadersWithConfig(false, "*")(handler)

	// Test GET request with CORS disabled
	req := httptest.NewRequest("GET", "/dev/projects", nil)
	w := httptest.NewRecorder()
	corsHandler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Expected no CORS headers when disabled, but got Access-Control-Allow-Origin: '%s'", w.Header().Get("Access-Control-Allow-Origin"))
	}

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", w.Code)
	}
}
