package awsdevops

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
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

// SSOProfile returns the profile that the SDK uses, if that profile uses SSO.
// Without --profile or AWS_PROFILE, the SDK uses the default profile.
func SSOProfile(path, profile string) string {
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}
	if !HasSSOConfig(path, profile) {
		return ""
	}

	return profile
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
