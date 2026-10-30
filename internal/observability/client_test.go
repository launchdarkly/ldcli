package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/observability"
)

func TestQuery(t *testing.T) {
	serve := func(t *testing.T, status int, body string) (string, *http.Header) {
		var headers http.Header
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers = r.Header.Clone()
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		return server.URL, &headers
	}

	t.Run("sends the token and project and returns the data", func(t *testing.T) {
		url, headers := serve(t, http.StatusOK, `{"data":{"ok":true}}`)
		data, err := observability.NewClient("1.0.0").Query(context.Background(), observability.Request{
			AccessToken: "api-123", Endpoint: url, ProjectID: "p1", Query: "query { ok }",
		})

		require.NoError(t, err)
		assert.JSONEq(t, `{"ok":true}`, string(data))
		assert.Equal(t, "api-123", headers.Get("Gonfalon-Authorization"))
		assert.Equal(t, "p1", headers.Get("x-project-id"))
		assert.Empty(t, headers.Get("Authorization"))
		assert.Equal(t, "launchdarkly-cli/v1.0.0", headers.Get("User-Agent"))
	})

	t.Run("adds Bearer to a token that is not an access token", func(t *testing.T) {
		url, headers := serve(t, http.StatusOK, `{"data":{}}`)
		_, err := observability.NewClient("1").Query(context.Background(), observability.Request{AccessToken: "oauth-token", Endpoint: url})

		require.NoError(t, err)
		assert.Equal(t, "Bearer oauth-token", headers.Get("Gonfalon-Authorization"))
	})

	t.Run("returns GraphQL errors, also with a status that is not 2xx", func(t *testing.T) {
		url, _ := serve(t, http.StatusUnprocessableEntity, `{"errors":[{"message":"a"},{"message":"b"}]}`)
		_, err := observability.NewClient("1").Query(context.Background(), observability.Request{Endpoint: url})

		require.EqualError(t, err, "observability API error: a; b")
	})

	t.Run("returns the status for a response that is not JSON", func(t *testing.T) {
		url, _ := serve(t, http.StatusBadGateway, `<html>bad gateway</html>`)
		_, err := observability.NewClient("1").Query(context.Background(), observability.Request{Endpoint: url})

		require.EqualError(t, err, "observability API returned 502 Bad Gateway")
	})

	t.Run("returns an error for a response with no data", func(t *testing.T) {
		url, _ := serve(t, http.StatusOK, `{"data":null}`)
		_, err := observability.NewClient("1").Query(context.Background(), observability.Request{Endpoint: url})

		require.EqualError(t, err, "observability response has no data")
	})
}

func TestEndpointFor(t *testing.T) {
	for baseURI, want := range map[string]string{
		"https://app.launchdarkly.com":    "https://pri.observability.app.launchdarkly.com",
		"https://app.launchdarkly.com/":   "https://pri.observability.app.launchdarkly.com",
		"https://ld-stg.launchdarkly.com": "https://pri.observability.ld-stg.launchdarkly.com",
		"https://app.ld.catamorphic.com":  "https://pri.observability.app.ld.catamorphic.com",
		"https://app.eu.launchdarkly.com": "https://pri.observability.app.eu.launchdarkly.com",
	} {
		got, err := observability.EndpointFor(baseURI)
		require.NoError(t, err, baseURI)
		assert.Equal(t, want, got, baseURI)
	}

	for _, baseURI := range []string{"http://localhost:8080", "https://launchdarkly.com.example.com", "not a url"} {
		_, err := observability.EndpointFor(baseURI)
		assert.Error(t, err, baseURI)
	}
}
