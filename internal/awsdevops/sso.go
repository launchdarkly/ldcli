package awsdevops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
)

// SelectedProfile returns the profile that the SDK uses. Without --profile, the
// SDK reads AWS_PROFILE, then AWS_DEFAULT_PROFILE, then uses the default
// profile. chosen is false if the default profile is only the fallback.
func SelectedProfile(profile string) (name string, chosen bool) {
	for _, name := range []string{profile, os.Getenv("AWS_PROFILE"), os.Getenv("AWS_DEFAULT_PROFILE")} {
		if name != "" {
			return name, true
		}
	}

	return "default", false
}

// SSOProfile returns the profile to pass to aws sso login, if the selected
// profile gets its credentials from SSO. A profile with source_profile gets
// them from its source profiles, so SSOProfile returns the first one of those
// that uses SSO.
func SSOProfile(ctx context.Context, profile string) string {
	name, _ := SelectedProfile(profile)
	cfg, err := awsconfig.LoadSharedConfigProfile(ctx, name, func(o *awsconfig.LoadSharedConfigOptions) {
		o.CredentialsFiles = []string{credentialsFilePath()}
		o.ConfigFiles = []string{configFilePath()}
	})
	if err != nil {
		return ""
	}
	for c := &cfg; c != nil; c = c.Source {
		if c.SSOSessionName != "" || c.SSOStartURL != "" {
			return c.Profile
		}
	}

	return ""
}

// SSOTokenInvalid reports an SSO token that is expired or not in the cache.
// Only then does aws sso login help.
func SSOTokenInvalid(err error) bool {
	var invalid *ssocreds.InvalidTokenError
	switch {
	case err == nil:
		return false
	case errors.As(err, &invalid):
		// A legacy profile with sso_start_url.
		return true
	case errors.Is(err, fs.ErrNotExist):
		// An sso_session profile with no cached token.
		return true
	}

	// An sso_session token that expired and cannot be refreshed. The SDK has
	// no error type for this case. A wrapper can hide the cause from Error, so
	// look at each error in the chain.
	for ; err != nil; err = errors.Unwrap(err) {
		if strings.Contains(err.Error(), "refresh cached SSO token failed") {
			return true
		}
	}

	return false
}
