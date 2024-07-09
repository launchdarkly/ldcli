package resources_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/resources"
)

func TestMakeUnauthenticatedRequest(t *testing.T) {
	t.Run("with a successful response", func(t *testing.T) {
		server := makeServer(t, http.StatusOK, `{"message": "success"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		response, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		require.NoError(t, err)
		assert.JSONEq(t, `{"message": "success"}`, string(response))
	})

	t.Run("with an error with a response body", func(t *testing.T) {
		server := makeServer(t, http.StatusBadRequest, `{"message": "an error"}`)
		defer server.Close()
		c := resources.NewClient("test-version")

		_, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		assert.EqualError(t, err, `{"message": "an error"}`)
	})

	t.Run("with a method not allowed status code and no body", func(t *testing.T) {
		server := makeServer(t, http.StatusMethodNotAllowed, "")
		defer server.Close()
		c := resources.NewClient("test-version")
		expectedResponse := `{
			"code": "method_not_allowed",
			"message": "method not allowed"
		}`

		_, err := c.MakeUnauthenticatedRequest("POST", server.URL, []byte(`{}`))

		assert.JSONEq(t, expectedResponse, err.Error())
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
