package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	// RelayPath is the path of the SDK relay in gonfalon, under the user's base
	// URI.
	RelayPath = "/internal/ldcli-setup-agent/sdk"
	// RelayTokenHeader carries the user's access token to the relay. The relay
	// cannot use Authorization, because the SDK puts its key there. The name
	// must not start with X-LD-: gonfalon removes client request headers with
	// that prefix, because its private auth headers use it.
	RelayTokenHeader = "LD-CLI-Token"
	// RelayPlaceholderKey is the SDK key that the CLI gives the AI SDK. The
	// relay replaces it with the real key on the way to catfood.
	RelayPlaceholderKey = "ldcli-placeholder"
)

// relayProxy is a loopback HTTP server between the AI SDK and the relay. It
// adds the access token to each request, so the token is never in an SDK
// option, an environment variable, or the OpenCode process. The AI SDK cannot
// add a header to its trace exports, which is the other reason for the proxy.
//
// The proxy accepts requests only under a random path, so another process on
// the machine, such as OpenCode, cannot use it to call the relay with the
// user's token.
type relayProxy struct {
	server   *http.Server
	listener net.Listener
	// baseURL is the proxy address with the random path, for example
	// http://127.0.0.1:52011/3f9a....
	baseURL string
}

// startRelayProxy starts the proxy. relayBase is the user's base URI, for
// example https://app.launchdarkly.com.
func startRelayProxy(relayBase, accessToken string) (*relayProxy, error) {
	target, err := url.Parse(strings.TrimRight(relayBase, "/") + RelayPath)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("the base URI %q is not a valid URL", relayBase)
	}
	secret, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not start the relay proxy: %w", err)
	}

	prefix := "/" + secret
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			rest := strings.TrimPrefix(r.In.URL.Path, prefix)
			r.Out.URL.Scheme = target.Scheme
			r.Out.URL.Host = target.Host
			r.Out.URL.Path = target.Path + rest
			r.Out.URL.RawPath = ""
			r.Out.Host = target.Host
			r.Out.Header.Set(RelayTokenHeader, accessToken)
		},
		// Send each part of a response as it arrives. The flag stream is a
		// long-lived event stream, and a buffer would hold its events back.
		FlushInterval: -1,
		// The proxy logs to stderr by default, which writes into the wizard's
		// screen. A failed request reaches the SDK as an error anyway.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})

	p := &relayProxy{
		server: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ErrorLog:          log.New(io.Discard, "", 0),
		},
		listener: listener,
		baseURL:  "http://" + listener.Addr().String() + prefix,
	}
	go func() {
		if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = listener.Close()
		}
	}()
	return p, nil
}

// close stops the proxy and ends open connections, including the flag
// stream.
func (p *relayProxy) close(ctx context.Context) error {
	if err := p.server.Shutdown(ctx); err != nil {
		return p.server.Close()
	}
	return nil
}
