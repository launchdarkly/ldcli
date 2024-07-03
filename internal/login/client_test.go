package login_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/launchdarkly/ldcli/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeRequest(t *testing.T) {
	t.Run("with a successful response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "launchdarkly-cli/vtest-version", r.Header.Get("User-Agent"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message": "success"}`))
		}))
		defer server.Close()
		c := login.NewClient("test-version")

		response, err := c.MakeRequest("POST", server.URL, []byte(`{}`))

		require.NoError(t, err)
		assert.JSONEq(t, `{"message": "success"}`, string(response))
	})

	t.Run("with an error with a response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "launchdarkly-cli/vtest-version", r.Header.Get("User-Agent"))
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "an error"}`))
		}))
		defer server.Close()
		c := login.NewClient("test-version")

		_, err := c.MakeRequest("POST", server.URL, []byte(`{}`))

		assert.EqualError(t, err, `{"message": "an error"}`)
	})

	t.Run("with a method not allowed status code and no body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "launchdarkly-cli/vtest-version", r.Header.Get("User-Agent"))
			w.WriteHeader(http.StatusMethodNotAllowed)
		}))
		defer server.Close()
		c := login.NewClient("test-version")
		expectedResponse := `{
			"code": "method_not_allowed",
			"message": "method not allowed"
		}`

		_, err := c.MakeRequest("POST", server.URL, []byte(`{}`))

		assert.EqualError(t, err, expectedResponse)
	})
}
