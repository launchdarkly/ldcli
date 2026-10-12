package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

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
