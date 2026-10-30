// Package observability calls the LaunchDarkly observability GraphQL API, the API
// that the LaunchDarkly UI and the hosted LaunchDarkly MCP server use for logs,
// traces, aggregations, and keys.
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultEndpoint is the observability API of the LaunchDarkly production instance.
	DefaultEndpoint = "https://pri.observability.app.launchdarkly.com"

	// endpointPrefix is the host prefix of the observability API on each LaunchDarkly
	// instance. For example, app.launchdarkly.com has its API at
	// pri.observability.app.launchdarkly.com.
	endpointPrefix = "pri.observability."

	defaultTimeout   = 60 * time.Second
	maxErrorMessages = 10
	maxErrorLength   = 512
)

// launchDarklyDomains are the domains of LaunchDarkly instances. The CLI derives the
// observability API host only from a base URI on one of these domains.
var launchDarklyDomains = []string{"launchdarkly.com", "catamorphic.com"}

// Request is one GraphQL operation for one observability project.
type Request struct {
	AccessToken string
	Endpoint    string
	// ProjectID is the LaunchDarkly project _id. The observability API uses it as the
	// project identifier.
	ProjectID string
	Query     string
	Variables map[string]any
}

// Client runs observability GraphQL operations.
type Client interface {
	Query(ctx context.Context, req Request) (json.RawMessage, error)
}

// HTTPClient is the Client that sends requests over HTTP.
type HTTPClient struct {
	cliVersion string
	httpClient *http.Client
}

var _ Client = HTTPClient{}

func NewClient(cliVersion string) HTTPClient {
	return HTTPClient{
		cliVersion: cliVersion,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// Query sends the operation and returns the "data" object of the response. It
// returns an error if the response has GraphQL errors or a status that is not 2xx.
func (c HTTPClient) Query(ctx context.Context, req Request) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"query":     req.Query,
		"variables": req.Variables,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding observability request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building observability request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", fmt.Sprintf("launchdarkly-cli/v%s", c.cliVersion))
	httpReq.Header.Set("Gonfalon-Authorization", authorizationHeader(req.AccessToken))
	httpReq.Header.Set("x-project-id", req.ProjectID)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("requesting observability API: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading observability response: %w", err)
	}

	var decoded struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	decodeErr := json.Unmarshal(body, &decoded)

	if len(decoded.Errors) > 0 {
		messages := make([]string, 0, min(len(decoded.Errors), maxErrorMessages))
		for _, e := range decoded.Errors[:min(len(decoded.Errors), maxErrorMessages)] {
			messages = append(messages, truncate(strings.TrimSpace(e.Message), maxErrorLength))
		}
		return nil, fmt.Errorf("observability API error: %s", strings.Join(messages, "; "))
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("observability API returned %d %s", res.StatusCode, http.StatusText(res.StatusCode))
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decoding observability response: %w", decodeErr)
	}
	if len(decoded.Data) == 0 || string(decoded.Data) == "null" {
		return nil, fmt.Errorf("observability response has no data")
	}

	return decoded.Data, nil
}

// EndpointFor gives the observability API endpoint for a LaunchDarkly base URI. It
// returns an error if the base URI is not on a LaunchDarkly domain, because the CLI
// cannot know where a local or proxied stack serves its observability API.
func EndpointFor(baseURI string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURI))
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("cannot derive the observability API from base URI %q, set --observability-url", baseURI)
	}
	host := parsed.Hostname()
	for _, domain := range launchDarklyDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return "https://" + endpointPrefix + host, nil
		}
	}

	return "", fmt.Errorf("cannot derive the observability API from base URI %q, set --observability-url", baseURI)
}

// authorizationHeader sends access tokens (api-...) as they are, and adds "Bearer"
// to other tokens, the same as the hosted LaunchDarkly MCP server.
func authorizationHeader(token string) string {
	if strings.HasPrefix(token, "api-") || strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}

	return "Bearer " + token
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n]) + "..."
}
