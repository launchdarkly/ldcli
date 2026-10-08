// Package api is the LaunchDarkly REST client for sync. It reads and writes
// config variations, tools, skills, model configs, and sync manifests.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/launchdarkly/ldcli/internal/resources"
)

// Client calls the LaunchDarkly REST API with one access token.
type Client struct {
	transport   resources.Client
	accessToken string
	baseURI     string
}

// NewClient creates a client for the API at baseURI.
func NewClient(transport resources.Client, accessToken, baseURI string) Client {
	return Client{transport: transport, accessToken: accessToken, baseURI: baseURI}
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
	response, err := client.transport.MakeRequest(client.accessToken, http.MethodGet, endpoint, "", query, nil, false)
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
	response, err := client.transport.MakeRequest(client.accessToken, request.method, endpoint, "application/json", nil, body, false)
	if err != nil {
		return nil, newMutationError(request.action, request.resource, request.key, err)
	}
	return response, nil
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
