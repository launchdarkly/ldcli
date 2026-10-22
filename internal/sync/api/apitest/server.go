// Package apitest serves a test LaunchDarkly API over HTTP.
package apitest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// CloseConnection makes the server close the connection without a response.
const CloseConnection = -1

// Request is one request that the test server received.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// Handler answers one request with a status and a body, or with CloseConnection.
type Handler func(Request) (status int, body []byte)

// JSON returns status 200 and the JSON form of value.
func JSON(value any) (int, []byte) {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return http.StatusOK, body
}

// NewServer serves handler until the test ends. It turns off keep-alive, so the client never reuses a closed connection.
func NewServer(t testing.TB, handler Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		status, response := handler(Request{
			Method: request.Method,
			Path:   request.URL.Path,
			Query:  request.URL.Query(),
			Header: request.Header.Clone(),
			Body:   body,
		})
		if status == CloseConnection {
			closeConnection(t, writer)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write(response)
	}))
	server.Config.SetKeepAlivesEnabled(false)
	server.Start()
	t.Cleanup(server.Close)
	return server
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
