package symbols

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// A run that skipped everything still prints "Successfully uploaded", so the notice
// is what tells someone re-uploading to repair an object that nothing was sent.
func TestReportUploadSummaryNotesSkippedFiles(t *testing.T) {
	stdout, stderr := captureOutput(t, func() { reportUploadSummary(2) })
	assert.Contains(t, stdout, "(2 already present)")
	assert.Contains(t, stderr, noSkipExistingFlag, "the notice should name the opt-out")

	stdout, stderr = captureOutput(t, func() { reportUploadSummary(0) })
	assert.Contains(t, stdout, "Successfully uploaded all symbols")
	assert.Empty(t, stderr, "nothing was skipped, so there is nothing to warn about")
}

// captureOutput runs fn with os.Stdout and os.Stderr redirected, returning what each
// received. Both are small enough here to stay inside the pipe buffer.
func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	fn()
	os.Stdout, os.Stderr = origOut, origErr

	require.NoError(t, outW.Close())
	require.NoError(t, errW.Close())

	out, err := io.ReadAll(outR)
	require.NoError(t, err)
	errOut, err := io.ReadAll(errR)
	require.NoError(t, err)
	return string(out), string(errOut)
}

func TestGetSymbolUploadUrlsRequestsDedup(t *testing.T) {
	srv, got := dedupTestServer(t, `{"data":{"get_symbol_upload_urls_ld":["","https://u/2"]}}`)

	urls, err := getSymbolUploadUrls("key", "proj", []string{"a", "b"}, nil, srv.URL, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"", "https://u/2"}, urls)

	require.Len(t, *got, 1)
	req := (*got)[0]
	assert.Contains(t, req.Query, skipExistingArgument, "the dedup document should be sent")
	assert.Equal(t, true, req.Variables[skipExistingArgument])
	assert.NotContains(t, req.Variables, digestsArgument, "no key needed a digest")
}

// A digest is what lets the backend settle a key that isn't derived from its own
// contents, so it has to arrive aligned with the paths it describes.
func TestGetSymbolUploadUrlsSendsDigests(t *testing.T) {
	srv, got := dedupTestServer(t, `{"data":{"get_symbol_upload_urls_ld":["https://u/1",""]}}`)

	digest := contentDigest([]byte("sources"))
	_, err := getSymbolUploadUrls("key", "proj", []string{"mapping.txt", "sources.srcbundle"}, []string{"", digest}, srv.URL, true)
	require.NoError(t, err)

	require.Len(t, *got, 1)
	req := (*got)[0]
	assert.Contains(t, req.Query, digestsArgument)
	assert.Equal(t, []interface{}{"", digest}, req.Variables[digestsArgument])
}

// An all-empty slice is padding, and the backend takes either one digest per path or
// none at all, so it is left off rather than sent.
func TestGetSymbolUploadUrlsOmitsEmptyDigests(t *testing.T) {
	srv, got := dedupTestServer(t, `{"data":{"get_symbol_upload_urls_ld":["https://u/1"]}}`)

	_, err := getSymbolUploadUrls("key", "proj", []string{"a"}, []string{""}, srv.URL, true)
	require.NoError(t, err)

	require.Len(t, *got, 1)
	assert.NotContains(t, (*got)[0].Variables, digestsArgument)
}

func TestGetSymbolUploadUrlsOmitsDedupWhenDisabled(t *testing.T) {
	srv, got := dedupTestServer(t, `{"data":{"get_symbol_upload_urls_ld":["https://u/1"]}}`)

	urls, err := getSymbolUploadUrls("key", "proj", []string{"a"}, []string{contentDigest([]byte("a"))}, srv.URL, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://u/1"}, urls)

	require.Len(t, *got, 1)
	req := (*got)[0]
	assert.NotContains(t, req.Query, skipExistingArgument, "--no-skip-existing must not ask for dedup")
	assert.NotContains(t, req.Variables, skipExistingArgument)
	assert.NotContains(t, req.Variables, digestsArgument)
}

// A backend that predates the arguments rejects the whole query, and an updated CLI
// must still work against it by re-asking without dedup. Validation names whichever
// argument it reached, so either one has to trigger the retry.
func TestGetSymbolUploadUrlsFallsBackWhenArgumentUnknown(t *testing.T) {
	for _, unknown := range []string{skipExistingArgument, digestsArgument} {
		t.Run(unknown, func(t *testing.T) {
			srv, got := dedupTestServer(t,
				fmt.Sprintf(`{"errors":[{"message":"Unknown argument \"%s\" on field \"Query.get_symbol_upload_urls_ld\"."}]}`, unknown),
				`{"data":{"get_symbol_upload_urls_ld":["https://u/1"]}}`,
			)

			urls, err := getSymbolUploadUrls("key", "proj", []string{"a"}, []string{contentDigest([]byte("a"))}, srv.URL, true)
			require.NoError(t, err)
			assert.Equal(t, []string{"https://u/1"}, urls)

			require.Len(t, *got, 2, "expected one rejected attempt and one retry")
			assert.Contains(t, (*got)[0].Query, skipExistingArgument)
			assert.NotContains(t, (*got)[1].Query, skipExistingArgument, "the retry must drop the arguments")
			assert.NotContains(t, (*got)[1].Variables, digestsArgument)
		})
	}
}

// Only the unknown-argument case is retried, so a credential or project error still
// reaches the caller instead of being retried as a plain upload.
func TestGetSymbolUploadUrlsDoesNotRetryOtherErrors(t *testing.T) {
	srv, got := dedupTestServer(t, `{"errors":[{"message":"error querying project"}]}`)

	_, err := getSymbolUploadUrls("key", "proj", []string{"a"}, nil, srv.URL, true)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "error querying project"))
	assert.Len(t, *got, 1, "a non-argument error must not trigger a retry")
}

// The digest has to be the one S3 reports as a one-part object's ETag, or the backend
// can never match it against what it stores.
func TestContentDigestMatchesS3ETagForm(t *testing.T) {
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", contentDigest(nil))
	assert.Equal(t, "5d41402abc4b2a76b9719d911017c592", contentDigest([]byte("hello")))
}
