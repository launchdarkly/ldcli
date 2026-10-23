package awsdevops

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
)

// ConfigFilePath returns the shared AWS config file path.
func ConfigFilePath() string {
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".aws", "config")
}

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

// SSOProfile returns the profile that the SDK uses, if that profile uses SSO.
func SSOProfile(path, profile string) string {
	name, _ := SelectedProfile(profile)
	if !HasSSOConfig(path, name) {
		return ""
	}

	return name
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

// NoProfileSelected reports that the SDK has no profile and no credentials
// from the environment, and that the default profile does not exist.
func NoProfileSelected(profile string) bool {
	if _, chosen := SelectedProfile(profile); chosen || os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		return false
	}

	return !slices.Contains(knownProfiles(), "default")
}

// LoginTokenInvalid reports an aws login token that is expired, not in the
// cache, or not valid after a password change. Only then does aws login help.
// The SDK has no error type for these cases, but each message asks the user
// to authenticate again.
func LoginTokenInvalid(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if strings.Contains(err.Error(), "please reauthenticate") {
			return true
		}
	}

	return false
}

// HasSSOConfig reports whether the profile uses SSO.
func HasSSOConfig(path, profile string) bool {
	return hasProfileKey(path, profile, "sso_session", "sso_start_url")
}

// HasLoginSession reports whether the profile uses credentials from aws login.
func HasLoginSession(path, profile string) bool {
	return hasProfileKey(path, profile, "login_session")
}

// hasProfileKey reports whether the profile sets one of the keys.
func hasProfileKey(path, profile string, keys ...string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	inProfile := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inProfile = sectionProfile(line[1:len(line)-1]) == profile
			continue
		}
		if !inProfile {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if slices.Contains(keys, strings.TrimSpace(key)) {
			return true
		}
	}

	return false
}

// sectionProfile returns the profile a section names.
func sectionProfile(section string) string {
	section = strings.TrimSpace(section)
	if section == "default" {
		return "default"
	}
	name, ok := strings.CutPrefix(section, "profile ")
	if !ok {
		return ""
	}

	return strings.TrimSpace(name)
}
