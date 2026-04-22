package sdk

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
)

func newCorsTestServer(t *testing.T, methods ...string) *httptest.Server {
	t.Helper()
	router := mux.NewRouter()
	router.Use(CorsHeadersForMethods(methods...))
	router.Methods(append(methods, http.MethodOptions)...).
		PathPrefix("/test").
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	return httptest.NewServer(router)
}

// TestCorsHeaders verifies that the CORS middleware correctly supports the configured methods.
func TestCorsHeaders(t *testing.T) {
	var allowedMethods = []string{http.MethodGet, "REPORT"}
	server := newCorsTestServer(t, allowedMethods...)
	defer server.Close()

	const origin = "http://example.com"

	tests := []struct {
		name          string
		requestMethod string
		wantAllowed   bool
	}{
		{
			name:          "GET is allowed",
			requestMethod: http.MethodGet,
			wantAllowed:   true,
		},
		{
			name:          "REPORT is allowed",
			requestMethod: "REPORT",
			wantAllowed:   true,
		},
		{
			name:          "PUT is not allowed",
			requestMethod: http.MethodPut,
			wantAllowed:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodOptions, server.URL+"/test", nil)
			assert.NoError(t, err)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", tt.requestMethod)

			resp, err := http.DefaultClient.Do(req)
			assert.NoError(t, err)
			defer resp.Body.Close()

			if tt.wantAllowed {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
			} else {
				assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
				assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
			}
		})
	}
}
