package awsdevops

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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

// HasSSOConfig reports whether the profile uses SSO.
func HasSSOConfig(path, profile string) bool {
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
		switch strings.TrimSpace(key) {
		case "sso_session", "sso_start_url":
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
