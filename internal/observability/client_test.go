package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthHeader(t *testing.T) {
	assert.Equal(t, "api-123", AuthHeader("api-123"))
	assert.Equal(t, "Bearer abc", AuthHeader("abc"))
	assert.Equal(t, "Bearer abc", AuthHeader("Bearer abc"))
}

func TestDoReturnsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized\n"))
	}))
	defer server.Close()

	_, err := Client{BackendURL: server.URL, AccessToken: "api-1"}.Do("p", "query X { x }", nil)
	require.Error(t, err)
	assert.Equal(t, "observability API returned 401: unauthorized", err.Error())
}
