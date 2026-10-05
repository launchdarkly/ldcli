package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBackendURL is the production observability GraphQL endpoint.
const DefaultBackendURL = "https://pri.observability.app.launchdarkly.com"

const requestTimeout = 60 * time.Second

// Client executes GraphQL operations against the observability backend. It authenticates with
// a LaunchDarkly access token, which the backend validates against LaunchDarkly.
type Client struct {
	BackendURL  string
	AccessToken string
	CLIVersion  string
	HTTPClient  *http.Client
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// AuthHeader returns the value the backend expects in the Gonfalon-Authorization header:
// personal and service access tokens are sent as-is, OAuth tokens as bearer tokens.
func AuthHeader(token string) string {
	if strings.HasPrefix(token, "api-") || strings.HasPrefix(token, "Bearer ") {
		return token
	}
	return "Bearer " + token
}

// Do runs a GraphQL operation scoped to the given observability project ID and returns the
// response's data field.
func (c Client) Do(projectID, query string, variables map[string]interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.BackendURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gonfalon-Authorization", AuthHeader(c.AccessToken))
	req.Header.Set("x-ld-project-id", projectID)
	req.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.CLIVersion))

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("observability API returned %d: %s", res.StatusCode, strings.TrimSpace(string(resBody)))
	}

	var gqlRes graphQLResponse
	if err := json.Unmarshal(resBody, &gqlRes); err != nil {
		return nil, fmt.Errorf("unable to parse observability API response: %w", err)
	}
	if len(gqlRes.Errors) > 0 {
		msgs := make([]string, 0, len(gqlRes.Errors))
		for _, e := range gqlRes.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("observability API error: %s", strings.Join(msgs, "; "))
	}

	return gqlRes.Data, nil
}
