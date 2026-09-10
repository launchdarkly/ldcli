package sync

import (
	"fmt"
	"io"
	"net/url"

	"github.com/launchdarkly/ldcli/internal/resources"
)

type debugClient struct {
	next resources.Client
	out  io.Writer
}

func (c debugClient) MakeRequest(
	accessToken string,
	method string,
	endpoint string,
	contentType string,
	query url.Values,
	body []byte,
	isBeta bool,
) ([]byte, error) {
	writeRequestDebug(c.out, method, endpoint, query, body)

	return c.next.MakeRequest(accessToken, method, endpoint, contentType, query, body, isBeta)
}

func (c debugClient) MakeUnauthenticatedRequest(method, endpoint string, body []byte) ([]byte, error) {
	return c.next.MakeUnauthenticatedRequest(method, endpoint, body)
}

func writeRequestDebug(out io.Writer, method, endpoint string, query url.Values, body []byte) {
	path := endpoint
	if parsed, err := url.Parse(endpoint); err == nil {
		values := parsed.Query()
		for name, entries := range query {
			for _, entry := range entries {
				values.Add(name, entry)
			}
		}
		parsed.RawQuery = values.Encode()
		path = parsed.RequestURI()
	}

	fmt.Fprintf(out, "HTTP request\nMethod: %s\nPath: %s\nBody:\n%s\n", method, path, body)
}
