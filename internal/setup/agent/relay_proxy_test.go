package agent

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRelay records the last request and answers like the relay.
func fakeRelay(t *testing.T) (*httptest.Server, *http.Request) {
	t.Helper()
	got := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(context.Background())
		if strings.HasSuffix(r.URL.Path, "/stream/all") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: put\ndata: {}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"flags":{}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func startTestProxy(t *testing.T, relayBase string) *relayProxy {
	t.Helper()
	p, err := startRelayProxy(relayBase, "api-secret-token")
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.close(context.Background()) })
	return p
}

func TestRelayProxy_AddsTheTokenAndMapsThePath(t *testing.T) {
	relay, got := fakeRelay(t)
	p := startTestProxy(t, relay.URL+"/")

	req, err := http.NewRequest(http.MethodGet, p.baseURL+"/polling/sdk/latest-all?basis=1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", RelayPlaceholderKey)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, `{"flags":{}}`, string(body))
	assert.Equal(t, RelayPath+"/polling/sdk/latest-all", got.URL.Path)
	assert.Equal(t, "basis=1", got.URL.RawQuery)
	assert.Equal(t, "api-secret-token", got.Header.Get(RelayTokenHeader))
	assert.Equal(t, RelayPlaceholderKey, got.Header.Get("Authorization"))
}

func TestRelayProxy_RefusesRequestsWithoutTheSecretPath(t *testing.T) {
	relay, got := fakeRelay(t)
	p := startTestProxy(t, relay.URL)
	host := strings.TrimSuffix(p.baseURL, p.baseURL[strings.LastIndex(p.baseURL, "/"):])

	resp, err := http.Get(host + "/polling/sdk/latest-all")
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Empty(t, got.Header.Get(RelayTokenHeader), "a request without the secret never reaches the relay")
}

func TestRelayProxy_StreamsWithoutBuffering(t *testing.T) {
	relay, _ := fakeRelay(t)
	p := startTestProxy(t, relay.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/stream/all", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// The relay holds the stream open, so this line arrives only if the proxy
	// sends it on at once.
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "event: put\n", line)
}

func TestStartRelayProxy_RejectsAnInvalidBaseURI(t *testing.T) {
	_, err := startRelayProxy("not a url", "token")

	assert.Error(t, err)
}
