package awsdevops

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Profile is a profile in the shared AWS files. The fields other than Name
// are empty if the profile does not set them. SignedIn is true for an SSO
// profile with a cached token that is not expired.
type Profile struct {
	Name     string
	Account  string
	Role     string
	Region   string
	SignedIn bool
}

// DefaultFallback reports that the SDK has no profile and no credentials
// from the environment, so it uses the default profile. It checks only
// AWS_ACCESS_KEY_ID. Other ambient credentials, for example web identity or
// instance metadata, also return true. That is intentional: the caller asks
// for a profile only after NewClients returns ErrNoCredentials, and NewClients
// finds those credentials.
func DefaultFallback(profile string) bool {
	_, chosen := SelectedProfile(profile)

	return !chosen && os.Getenv("AWS_ACCESS_KEY_ID") == ""
}

// Profiles returns the profiles in the shared AWS files. The signed-in
// profiles come first, for example after aws sso login outside ldcli. A token
// belongs to an SSO session, so all the profiles that share a session are
// signed in. tokenPath gives the cache file for a session name or a start URL.
func Profiles(tokenPath func(key string) (string, error), now time.Time) []Profile {
	var signedIn, others []Profile
	valid := map[string]bool{}
	for _, section := range readProfiles() {
		profile := Profile{
			Name:    section.name,
			Account: section.keys["sso_account_id"],
			Role:    section.keys["sso_role_name"],
			Region:  section.keys["region"],
		}
		if profile.Account == "" && profile.Role == "" {
			profile.Account, profile.Role = roleARNParts(section.keys["role_arn"])
		}
		key := section.keys["sso_session"]
		if key == "" {
			key = section.keys["sso_start_url"]
		}
		if key != "" {
			ok, checked := valid[key]
			if !checked {
				ok = tokenValid(tokenPath, key, now)
				valid[key] = ok
			}
			profile.SignedIn = ok
		}
		if profile.SignedIn {
			signedIn = append(signedIn, profile)
		} else {
			others = append(others, profile)
		}
	}

	return append(signedIn, others...)
}

// roleARNParts returns the account and role name of a role ARN, for example
// arn:aws:iam::123456789012:role/team/Operator.
func roleARNParts(arn string) (account, role string) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || !strings.HasPrefix(parts[5], "role/") {
		return "", ""
	}

	return parts[4], parts[5][strings.LastIndex(parts[5], "/")+1:]
}

// tokenValid reports a cached SSO access token that is not expired.
func tokenValid(tokenPath func(string) (string, error), key string, now time.Time) bool {
	path, err := tokenPath(key)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var token struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   string `json:"expiresAt"`
	}
	if json.Unmarshal(data, &token) != nil || token.AccessToken == "" {
		return false
	}
	expires, err := time.Parse(time.RFC3339, token.ExpiresAt)

	return err == nil && now.Before(expires)
}

// profileSection is one profile and its keys.
type profileSection struct {
	name string
	keys map[string]string
}

// profileKeys are the keys that readProfiles keeps. It keeps no other key, so
// a secret access key never leaves the file.
var profileKeys = []string{"sso_account_id", "sso_role_name", "sso_session", "sso_start_url", "region", "role_arn"}

// readProfiles returns the profiles in the credentials file and then the
// config file, in file order. A profile in both files has the keys of both.
func readProfiles() []profileSection {
	var sections []profileSection
	index := map[string]int{}

	read := func(path string, nameOf func(string) string) {
		file, err := os.Open(path)
		if err != nil {
			return
		}
		defer func() { _ = file.Close() }()

		current := -1
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				current = -1
				name := nameOf(line[1 : len(line)-1])
				if name == "" {
					continue
				}
				i, ok := index[name]
				if !ok {
					i = len(sections)
					index[name] = i
					sections = append(sections, profileSection{name: name, keys: map[string]string{}})
				}
				current = i
				continue
			}
			if current < 0 {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if ok && value != "" && slices.Contains(profileKeys, key) {
				sections[current].keys[key] = value
			}
		}
	}

	// The credentials file names a profile without the "profile " prefix.
	read(credentialsFilePath(), func(section string) string {
		section = strings.TrimSpace(section)
		if strings.ContainsRune(section, ' ') {
			return ""
		}

		return section
	})
	read(ConfigFilePath(), sectionProfile)

	return sections
}

// credentialsFilePath returns the shared AWS credentials file path.
func credentialsFilePath() string {
	if v := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".aws", "credentials")
}
