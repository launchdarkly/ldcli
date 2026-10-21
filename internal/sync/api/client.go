// Package api is the LaunchDarkly REST client for sync. It reads and writes
// config variations, tools, skills, model configs, and sync manifests.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// SyncSourceHeader is the request header that marks a write by sync. Its
// value is the sync source of the repository, for example
// "git:github.com/acme/app".
const SyncSourceHeader = "LD-Sync-Source"

const apiVersion = "20240415"

// Transport keeps the User-Agent header of each sync request. The zero value
// sends no User-Agent header.
type Transport struct {
	userAgent string
}

// NewTransport creates a transport whose User-Agent header names cliVersion.
func NewTransport(cliVersion string) Transport {
	return Transport{userAgent: fmt.Sprintf("launchdarkly-cli/v%s", cliVersion)}
}

// Options are the values of one command that a Client uses.
type Options struct {
	// BaseURI is the LaunchDarkly URL, for example "https://app.launchdarkly.com".
	BaseURI     string
	AccessToken string
	// SyncSource is the sync source of the repository, for example
	// "git:github.com/acme/app". Each write sends it in SyncSourceHeader. If
	// SyncSource is empty, a write sends no sync header.
	SyncSource string
}

// Client calls the LaunchDarkly REST API with one access token.
type Client struct {
	transport   Transport
	baseURI     string
	accessToken string
	syncSource  string
}

// Client creates a client for the API at options.BaseURI. The client uses
// the HTTP client of the transport.
func (transport Transport) Client(options Options) Client {
	return Client{
		transport:   transport,
		baseURI:     options.BaseURI,
		accessToken: options.AccessToken,
		syncSource:  options.SyncSource,
	}
}

// Page is one server-filtered page of catalog items.
type Page[T any] struct {
	Items      []T `json:"items"`
	TotalCount int `json:"totalCount"`
}

// mutation describes one write request. The action, resource, and key name
// the write in error messages, for example `create config variation "x"`.
type mutation struct {
	method   string
	action   string
	resource string
	key      string
	body     any
}

// read sends a GET request. If the request fails, the error starts with the
// description and suggests a fix when the resource does not exist in projectKey.
func (client Client) read(description, projectKey string, query url.Values, path ...string) ([]byte, error) {
	endpoint, err := client.endpoint(path...)
	if err != nil {
		return nil, err
	}
	response, err := client.do(http.MethodGet, endpoint, query, nil)
	if err != nil {
		return nil, contextualAPIError(err, description, projectKey)
	}
	return response, nil
}

// mutate sends one JSON write request. The returned error records whether
// the write can have succeeded without a response.
func (client Client) mutate(request mutation, path ...string) ([]byte, error) {
	body, err := json.Marshal(request.body)
	if err != nil {
		return nil, fmt.Errorf("encode %s %q: %w", request.resource, request.key, err)
	}
	endpoint, err := client.endpoint(path...)
	if err != nil {
		return nil, err
	}
	response, err := client.do(request.method, endpoint, nil, body)
	if err != nil {
		return nil, newMutationError(request.action, request.resource, request.key, err)
	}
	return response, nil
}

// do sends one request and returns the response body. A request with a body
// sends it as JSON. A request that is not a GET is a write. A write sends the
// sync source header when the client has a sync source. If LaunchDarkly sends a
// status of 400 or more, the error text is the JSON API error.
func (client Client) do(method, endpoint string, query url.Values, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", client.accessToken)
	request.Header.Set("User-Agent", client.transport.userAgent)
	request.Header.Set("LD-API-Version", apiVersion)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		request.Header.Set(SyncSourceHeader, client.syncSource)
	}
	request.URL.RawQuery = query.Encode()

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, statusError(response.StatusCode, responseBody, request.URL.Scheme+"://"+request.URL.Host)
	}
	return responseBody, nil
}

func (client Client) endpoint(path ...string) (string, error) {
	endpoint, err := url.JoinPath(client.baseURI, path...)
	if err != nil {
		return "", fmt.Errorf("build API endpoint: %w", err)
	}
	return endpoint, nil
}

// projectPath returns the path of a resource in one project.
func projectPath(projectKey string, path ...string) []string {
	return append([]string{"api/v2/projects", projectKey}, path...)
}

// pageQuery returns the query values for one catalog page.
func pageQuery(limit, offset int) url.Values {
	return url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
}

// decodeJSON decodes one response. The description names the response in errors.
func decodeJSON[T any](data []byte, description string) (T, error) {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode %s: %w", description, err)
	}
	return value, nil
}
