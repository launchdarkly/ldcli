package awsdevops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ServiceTokenRole is the base role of the service tokens this command
// creates. The agent reads projects and flags and toggles them, which the
// writer role covers without granting account administration.
const ServiceTokenRole = "writer"

// ErrInvalidAccessToken is the sentinel for a token LaunchDarkly rejects.
var ErrInvalidAccessToken = errors.New("LaunchDarkly rejected the access token")

var ldHTTPClient = &http.Client{Timeout: 15 * time.Second}

// ValidateAccessToken checks a token against the LaunchDarkly API before AWS
// stores it. AWS keeps the token write-only, so replacing a working
// registration with an unusable token cannot be undone.
func ValidateAccessToken(ctx context.Context, baseURI, token string) error {
	if baseURI == "" {
		baseURI = DefaultLDBaseURI
	}
	endpoint, err := url.JoinPath(baseURI, "api/v2/caller-identity")
	if err != nil {
		return fmt.Errorf("invalid LaunchDarkly base URI %q: %w", baseURI, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", token)

	res, err := ldHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to reach LaunchDarkly at %s to check the access token: %w", baseURI, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return fmt.Errorf(
			"%w. Create a service token at %s and pass it with --access-token",
			ErrInvalidAccessToken,
			AccessTokenURL(baseURI),
		)
	case res.StatusCode >= http.StatusBadRequest:
		return fmt.Errorf("unable to check the access token: LaunchDarkly returned %s", res.Status)
	}

	return nil
}

// ServiceToken is a token LaunchDarkly issued to this command.
type ServiceToken struct {
	ID    string `json:"_id"`
	Name  string `json:"name"`
	Value string `json:"token"`
}

// CreateServiceToken asks LaunchDarkly for a service token dedicated to the
// MCP server, so the agent's API calls are attributable to it rather than to
// the member who ran setup, and revoking the agent's access never takes the
// CLI's own token with it.
func CreateServiceToken(ctx context.Context, baseURI, token, name string) (ServiceToken, error) {
	if baseURI == "" {
		baseURI = DefaultLDBaseURI
	}
	endpoint, err := url.JoinPath(baseURI, "api/v2/tokens")
	if err != nil {
		return ServiceToken{}, fmt.Errorf("invalid LaunchDarkly base URI %q: %w", baseURI, err)
	}

	body, err := json.Marshal(map[string]any{
		"name":         name,
		"description":  "Created by the LaunchDarkly CLI for the AWS DevOps Agent MCP server",
		"role":         ServiceTokenRole,
		"serviceToken": true,
	})
	if err != nil {
		return ServiceToken{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ServiceToken{}, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")

	res, err := ldHTTPClient.Do(req)
	if err != nil {
		return ServiceToken{}, fmt.Errorf("unable to reach LaunchDarkly at %s to create a service token: %w", baseURI, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return ServiceToken{}, fmt.Errorf(
			"%w. Pass --access-token with a token that can create service tokens",
			ErrInvalidAccessToken,
		)
	case res.StatusCode == http.StatusForbidden:
		return ServiceToken{}, fmt.Errorf(
			"your LaunchDarkly token is not allowed to create access tokens. Ask an administrator "+
				"for that permission, or create a service token at %s and pass it with --mcp-access-token",
			AccessTokenURL(baseURI),
		)
	case res.StatusCode >= http.StatusBadRequest:
		return ServiceToken{}, fmt.Errorf("unable to create a LaunchDarkly service token: LaunchDarkly returned %s", res.Status)
	}

	var created ServiceToken
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		return ServiceToken{}, fmt.Errorf("unable to read the service token LaunchDarkly created: %w", err)
	}
	if created.Value == "" {
		return ServiceToken{}, errors.New("LaunchDarkly created a service token without returning its value")
	}

	return created, nil
}
