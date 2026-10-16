package resources_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestMakeUnauthenticatedRequest(t *testing.T) {
	t.Run("with a successful response returns body and no error", func(t *testing.T) {
		server := makeServer(t, http.StatusOK, `{"message": "success"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		response, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.NoError(t, err)
		assert.JSONEq(t, `{"message": "success"}`, string(response))
	})

	t.Run("with 204 No Content returns no error", func(t *testing.T) {
		server := makeServer(t, http.StatusNoContent, "")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("DELETE", server.URL, []byte(`{}`))

		require.NoError(t, err)
	})

	t.Run("with an error with a response body injects statusCode", func(t *testing.T) {
		server := makeServer(t, http.StatusBadRequest, `{"code":"invalid_request","message": "an error"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.Error(t, err)
		assert.JSONEq(t, `{"code":"invalid_request","message":"an error","statusCode":400}`, err.Error())
	})

	t.Run("with a body that already has statusCode does not overwrite it", func(t *testing.T) {
		server := makeServer(t, http.StatusBadRequest, `{"code":"invalid_request","message":"an error","statusCode":400}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.Error(t, err)
		assert.JSONEq(t, `{"code":"invalid_request","message":"an error","statusCode":400}`, err.Error())
	})

	t.Run("with a 401 error includes suggestion in body", func(t *testing.T) {
		server := makeServer(t, http.StatusUnauthorized, `{"code":"unauthorized","message":"invalid token"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, float64(401), errMap["statusCode"])
		assert.Contains(t, errMap["suggestion"], "ldcli login")
		assert.Contains(t, errMap["suggestion"], "settings/authorization")
	})

	t.Run("with a method not allowed status code and no body", func(t *testing.T) {
		server := makeServer(t, http.StatusMethodNotAllowed, "")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.Error(t, err)
		assert.JSONEq(t, `{
			"code": "method_not_allowed",
			"message": "Method Not Allowed",
			"statusCode": 405
		}`, err.Error())
	})

	t.Run("with a 404 and no body produces valid JSON with statusCode", func(t *testing.T) {
		server := makeServer(t, http.StatusNotFound, "")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("GET", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, "not_found", errMap["code"])
		assert.Equal(t, "Not Found", errMap["message"])
		assert.Equal(t, float64(404), errMap["statusCode"])
		assert.Contains(t, errMap["suggestion"], "ldcli projects list")
	})

	t.Run("with a 500 and no body produces valid JSON without suggestion", func(t *testing.T) {
		server := makeServer(t, http.StatusInternalServerError, "")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("GET", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, "internal_server_error", errMap["code"])
		assert.Equal(t, "Internal Server Error", errMap["message"])
		assert.Equal(t, float64(500), errMap["statusCode"])
		assert.Nil(t, errMap["suggestion"])
	})

	t.Run("with a 502 and no body produces valid JSON without suggestion", func(t *testing.T) {
		server := makeServer(t, http.StatusBadGateway, "")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("GET", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, "bad_gateway", errMap["code"])
		assert.Equal(t, float64(502), errMap["statusCode"])
		assert.Nil(t, errMap["suggestion"])
	})

	t.Run("with a 429 and body includes rate limit suggestion", func(t *testing.T) {
		server := makeServer(t, http.StatusTooManyRequests, `{"code":"rate_limited","message":"too many requests"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("GET", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, float64(429), errMap["statusCode"])
		assert.Contains(t, errMap["suggestion"], "Rate limited")
	})

	t.Run("with a non-JSON body produces synthetic JSON error", func(t *testing.T) {
		server := makeServer(t, http.StatusBadGateway, "<html><body>Bad Gateway</body></html>")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("GET", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, "bad_gateway", errMap["code"])
		assert.Equal(t, "<html><body>Bad Gateway</body></html>", errMap["message"])
		assert.Equal(t, float64(502), errMap["statusCode"])
	})

	t.Run("with a plain-text body produces synthetic JSON error with suggestion", func(t *testing.T) {
		server := makeServer(t, http.StatusNotFound, "page not found")
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("GET", server.URL, []byte(`{}`))

		require.Error(t, err)

		var errMap map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(err.Error()), &errMap))
		assert.Equal(t, "not_found", errMap["code"])
		assert.Equal(t, "page not found", errMap["message"])
		assert.Equal(t, float64(404), errMap["statusCode"])
		assert.Contains(t, errMap["suggestion"], "ldcli projects list")
	})
}

func TestMakeRequestAPIVersion(t *testing.T) {
	tests := map[string]struct {
		isBeta   bool
		expected string
	}{
		"non-beta requests send the current API version": {isBeta: false, expected: "20240415"},
		"beta requests send beta":                        {isBeta: true, expected: "beta"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var got []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Values("LD-API-Version")
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			c := resources.NewClient("test-version")

			_, err := c.MakeRequest("token", "GET", server.URL, "application/json", nil, nil, tt.isBeta)

			require.NoError(t, err)
			assert.Equal(t, []string{tt.expected}, got)
		})
	}
}

func TestMakeRequestWithHeaders(t *testing.T) {
	var received http.Header
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		query = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var c resources.HeaderClient = resources.NewClient("test-version")

	t.Run("sends the extra headers with the default headers", func(t *testing.T) {
		response, err := c.MakeRequestWithHeaders(
			"token", http.MethodPatch, server.URL, "application/json",
			url.Values{"limit": {"1"}}, []byte(`{}`), false,
			http.Header{"LD-Sync-Source": {"git:example.com/acme/app"}},
		)

		require.NoError(t, err)
		assert.JSONEq(t, `{"ok":true}`, string(response))
		assert.Equal(t, []string{"git:example.com/acme/app"}, received.Values("LD-Sync-Source"))
		assert.Equal(t, "token", received.Get("Authorization"))
		assert.Equal(t, "application/json", received.Get("Content-Type"))
		assert.Equal(t, "launchdarkly-cli/vtest-version", received.Get("User-Agent"))
		assert.Equal(t, "20240415", received.Get("LD-API-Version"))
		assert.Equal(t, "limit=1", query)
	})

	t.Run("an extra header replaces a default header", func(t *testing.T) {
		_, err := c.MakeRequestWithHeaders(
			"token", http.MethodGet, server.URL, "application/json", nil, nil, false,
			http.Header{"Ld-Api-Version": {"beta"}},
		)

		require.NoError(t, err)
		assert.Equal(t, []string{"beta"}, received.Values("LD-API-Version"))
	})

	t.Run("MakeRequest sends no sync source header", func(t *testing.T) {
		_, err := c.MakeRequest("token", http.MethodPatch, server.URL, "application/json", nil, []byte(`{}`), false)

		require.NoError(t, err)
		assert.Empty(t, received.Values("LD-Sync-Source"))
	})
}

func makeServer(t *testing.T, statusResponse int, responseBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "launchdarkly-cli/vtest-version", r.Header.Get("User-Agent"))
		w.WriteHeader(statusResponse)
		_, _ = w.Write([]byte(responseBody))
	}))
}
