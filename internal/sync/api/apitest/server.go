// Package apitest serves a test LaunchDarkly API over HTTP. Tests of sync
// code use it to send real HTTP requests through the sync API client.
package apitest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Request is one request that the test server received.
type Request struct {
	Method string
	// Path is the URL path, for example "/api/v2/projects/default/ai-tools".
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// Handler answers one request. The server sends the result as follows:
//
//   - If the error is nil, the server sends status 200 with the body.
//   - If the error text is a JSON object with a "statusCode" number, the
//     server sends that status with the error text as the body.
//   - For other errors, the server closes the connection without a
//     response. The client then cannot know if a write succeeded.
type Handler func(Request) ([]byte, error)

// NewServer starts a test server that sends each request to handler. The
// server closes after the test. The server does not keep connections open,
// so the client never sends a request again on a closed connection.
func NewServer(t testing.TB, handler Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		response, err := handler(Request{
			Method: request.Method,
			Path:   request.URL.Path,
			Query:  request.URL.Query(),
			Header: request.Header.Clone(),
			Body:   body,
		})
		if err == nil {
			_, _ = writer.Write(response)
			return
		}
		if status, ok := statusCode(err); ok {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(err.Error()))
			return
		}
		closeConnection(t, writer)
	}))
	server.Config.SetKeepAlivesEnabled(false)
	server.Start()
	t.Cleanup(server.Close)
	return server
}

// statusCode returns the "statusCode" field of an error whose text is a JSON
// object.
func statusCode(err error) (int, bool) {
	var response struct {
		StatusCode int `json:"statusCode"`
	}
	if json.Unmarshal([]byte(err.Error()), &response) != nil || response.StatusCode == 0 {
		return 0, false
	}
	return response.StatusCode, true
}

func closeConnection(t testing.TB, writer http.ResponseWriter) {
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		t.Errorf("the test server cannot close the connection")
		return
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		t.Errorf("take the connection: %v", err)
		return
	}
	_ = connection.Close()
}
