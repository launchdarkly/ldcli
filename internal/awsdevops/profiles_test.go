package awsdevops_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

var profilesNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

const validToken = `{"accessToken":"x","expiresAt":"2026-10-23T20:00:00Z"}`

// awsFiles points the SDK at a config file and a credentials file.
func awsFiles(t *testing.T, config, credentials string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	credentialsPath := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	require.NoError(t, os.WriteFile(credentialsPath, []byte(credentials), 0o600))
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)
}

// tokenCache writes a token for each key and returns a tokenPath for them.
func tokenCache(t *testing.T, tokens map[string]string) func(string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	for key, body := range tokens {
		path := filepath.Join(dir, filepath.Base(t.TempDir())+".json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		paths[key] = path
	}

	return func(key string) (string, error) {
		if path, ok := paths[key]; ok {
			return path, nil
		}

		return filepath.Join(dir, "missing.json"), nil
	}
}

func TestProfilesPutsSignedInProfilesFirst(t *testing.T) {
	awsFiles(t, "[sso-session corp]\nsso_start_url = https://corp.awsapps.com/start\n\n"+
		"[profile static]\nregion = us-east-1\n\n"+
		"[profile dev]\nsso_session = corp\nsso_account_id = 111111111111\nsso_role_name = Admin\nregion = us-west-2\n\n"+
		"[profile prod]\nsso_session = corp\nsso_account_id = 222222222222\nsso_role_name = Viewer\n", "")

	got := awsdevops.Profiles(tokenCache(t, map[string]string{"corp": validToken}), profilesNow)

	assert.Equal(t, []awsdevops.Profile{
		{Name: "dev", Account: "111111111111", Role: "Admin", Region: "us-west-2", SignedIn: true},
		{Name: "prod", Account: "222222222222", Role: "Viewer", SignedIn: true},
		{Name: "static", Region: "us-east-1"},
	}, got)
}

func TestProfilesUsesStartURLForLegacyProfile(t *testing.T) {
	awsFiles(t, "[default]\nsso_start_url = https://corp.awsapps.com/start\nregion = us-east-1\n", "")

	got := awsdevops.Profiles(tokenCache(t, map[string]string{"https://corp.awsapps.com/start": validToken}), profilesNow)

	assert.Equal(t, []awsdevops.Profile{{Name: "default", Region: "us-east-1", SignedIn: true}}, got)
}

func TestProfilesMarksExpiredMissingAndBadTokensSignedOut(t *testing.T) {
	awsFiles(t, "[profile old]\nsso_session = old\n\n[profile none]\nsso_session = none\n\n[profile bad]\nsso_session = bad\n", "")
	tokens := tokenCache(t, map[string]string{
		"old": `{"accessToken":"x","expiresAt":"2026-10-23T11:00:00Z"}`,
		"bad": `not json`,
	})

	assert.Equal(t, []awsdevops.Profile{{Name: "old"}, {Name: "none"}, {Name: "bad"}}, awsdevops.Profiles(tokens, profilesNow))
}

func TestProfilesMarksTokenPathErrorSignedOut(t *testing.T) {
	awsFiles(t, "[profile dev]\nsso_session = corp\n", "")
	tokens := func(string) (string, error) { return "", errors.New("no home") }

	assert.Equal(t, []awsdevops.Profile{{Name: "dev"}}, awsdevops.Profiles(tokens, profilesNow))
}

func TestProfilesReadsRoleARN(t *testing.T) {
	awsFiles(t, "[profile ops]\nrole_arn = arn:aws:iam::123456789012:role/team/Operator\nsource_profile = dev\n", "")

	assert.Equal(t, []awsdevops.Profile{{Name: "ops", Account: "123456789012", Role: "Operator"}},
		awsdevops.Profiles(tokenCache(t, nil), profilesNow))
}

func TestProfilesMergesTheCredentialsFileAndKeepsNoSecrets(t *testing.T) {
	awsFiles(t, "[profile dev]\nregion = us-east-1\n",
		"[keys]\naws_access_key_id = AKIAEXAMPLE\naws_secret_access_key = wJalrXUtnFEMI\n\n[dev]\naws_access_key_id = AKIAEXAMPLE\n")

	got := awsdevops.Profiles(tokenCache(t, nil), profilesNow)

	assert.Equal(t, []awsdevops.Profile{{Name: "keys"}, {Name: "dev", Region: "us-east-1"}}, got)
	assert.NotContains(t, fmt.Sprintf("%+v", got), "wJalrXUtnFEMI")
}

func TestDefaultFallback(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")

	assert.True(t, awsdevops.DefaultFallback(""))
	assert.False(t, awsdevops.DefaultFallback("dev"))

	t.Setenv("AWS_PROFILE", "dev")
	assert.False(t, awsdevops.DefaultFallback(""))

	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA")
	assert.False(t, awsdevops.DefaultFallback(""))
}
