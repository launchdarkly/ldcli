package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// NewProxy creates an http.Handler which will proxy requests to the baseUri and attach the provided accessToken
func NewProxy(accessToken, baseUri, version string) http.Handler {
	target, err := url.Parse(baseUri)
	if err != nil {
		log.Fatalf("unable to parse target url (%s): %v", baseUri, err)
	}
	proxy := new(httputil.ReverseProxy)
	proxy.Rewrite = func(r *httputil.ProxyRequest) {
		r.Out.Header.Set("Authorization", accessToken)
		r.Out.Header.Set("User-Agent", fmt.Sprintf("ldcli/dev-server@%s", version))
		r.SetURL(target)
		r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, "/proxy")
	}
	return proxy
}
