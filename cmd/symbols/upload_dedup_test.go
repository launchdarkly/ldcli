package symbols

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedRequest is one GraphQL request body a test server received.
type capturedRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

// dedupTestServer records every request and answers each with the given body.
// Responses are consumed in order so a test can drive the retry path.
func dedupTestServer(t *testing.T, responses ...string) (*httptest.Server, *[]capturedRequest) {
	t.Helper()
	var got []capturedRequest
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var captured capturedRequest
		require.NoError(t, json.Unmarshal(body, &captured))
		got = append(got, captured)

		response := responses[len(responses)-1]
		if i < len(responses) {
			response = responses[i]
		}
		i++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestAlreadyUploaded(t *testing.T) {
	// The empty string must never be mistaken for a URL to PUT to.
	assert.True(t, alreadyUploaded(""))
	assert.False(t, alreadyUploaded("https://example.com/presigned"))
}

func TestGetSymbolUploadUrlsRequestsDedup(t *testing.T) {
	srv, got := dedupTestServer(t, `{"data":{"get_symbol_upload_urls_ld":["","https://u/2"]}}`)

	urls, err := getSymbolUploadUrls("key", "proj", []string{"a", "b"}, srv.URL, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"", "https://u/2"}, urls)

	require.Len(t, *got, 1)
	req := (*got)[0]
	assert.Contains(t, req.Query, skipExistingArgument, "the dedup document should be sent")
	assert.Equal(t, true, req.Variables[skipExistingArgument])
}

func TestGetSymbolUploadUrlsOmitsDedupWhenDisabled(t *testing.T) {
	srv, got := dedupTestServer(t, `{"data":{"get_symbol_upload_urls_ld":["https://u/1"]}}`)

	urls, err := getSymbolUploadUrls("key", "proj", []string{"a"}, srv.URL, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://u/1"}, urls)

	require.Len(t, *got, 1)
	req := (*got)[0]
	assert.NotContains(t, req.Query, skipExistingArgument, "--no-skip-existing must not ask for dedup")
	assert.NotContains(t, req.Variables, skipExistingArgument)
}

// A backend that predates the argument rejects the whole query, and an updated CLI
// must still work against it by re-asking without dedup.
func TestGetSymbolUploadUrlsFallsBackWhenArgumentUnknown(t *testing.T) {
	srv, got := dedupTestServer(t,
		`{"errors":[{"message":"Unknown argument \"skip_existing\" on field \"Query.get_symbol_upload_urls_ld\"."}]}`,
		`{"data":{"get_symbol_upload_urls_ld":["https://u/1"]}}`,
	)

	urls, err := getSymbolUploadUrls("key", "proj", []string{"a"}, srv.URL, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://u/1"}, urls)

	require.Len(t, *got, 2, "expected one rejected attempt and one retry")
	assert.Contains(t, (*got)[0].Query, skipExistingArgument)
	assert.NotContains(t, (*got)[1].Query, skipExistingArgument, "the retry must drop the argument")
}

// Only the unknown-argument case is retried, so a credential or project error still
// reaches the caller instead of being retried as a plain upload.
func TestGetSymbolUploadUrlsDoesNotRetryOtherErrors(t *testing.T) {
	srv, got := dedupTestServer(t, `{"errors":[{"message":"error querying project"}]}`)

	_, err := getSymbolUploadUrls("key", "proj", []string{"a"}, srv.URL, true)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "error querying project"))
	assert.Len(t, *got, 1, "a non-argument error must not trigger a retry")
}
