package awsdevops_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const ssoSession = "\n[sso-session corp]\nsso_start_url = https://example.awsapps.com/start\nsso_region = us-east-1\n"

// writeConfig points the SDK at a temp config with this content.
func writeConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
}

func TestSSOProfileFindsSSOSession(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session = corp\nregion = us-east-1\n"+ssoSession)

	assert.Equal(t, "dev", awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileFindsLegacyStartURL(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_start_url=https://corp.awsapps.com/start\n")

	assert.Equal(t, "dev", awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileReadsDefaultSection(t *testing.T) {
	writeConfig(t, "[default]\nsso_session = corp\n"+ssoSession)

	assert.Equal(t, "default", awsdevops.SSOProfile(context.Background(), "default"))
}

func TestSSOProfileIgnoresNearMissProfile(t *testing.T) {
	writeConfig(t, "[profile dev]\nregion = us-east-1\n\n[profile dev-admin]\nsso_session = corp\n"+ssoSession)

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileIgnoresOtherProfileBeforeIt(t *testing.T) {
	writeConfig(t, "[profile dev-admin]\nsso_session = corp\n\n[profile dev]\nregion = us-east-1\n"+ssoSession)

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileIgnoresSSORegionOnly(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_region = us-east-1\nsso_account_id = 123\n")

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileIgnoresCommentedLines(t *testing.T) {
	writeConfig(t, "[profile dev]\n# sso_start_url = https://corp.awsapps.com/start\n; sso_session = corp\n")

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileIgnoresNearMissKey(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session_name = corp\nmy_sso_start_url = x\n")

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileIgnoresSessionSection(t *testing.T) {
	writeConfig(t, "[sso-session dev]\nsso_start_url = https://corp.awsapps.com/start\n")

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileIgnoresBareDefaultForOtherName(t *testing.T) {
	writeConfig(t, "[default]\nsso_session = corp\n"+ssoSession)

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

func TestSSOProfileHandlesMissingFile(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "dev"))
}

// A role profile gets its credentials from its source profile.
func TestSSOProfileFollowsSourceProfile(t *testing.T) {
	writeConfig(t, "[profile app]\nrole_arn = arn:aws:iam::123456789012:role/App\nsource_profile = base\n\n"+
		"[profile base]\nsso_session = corp\n"+ssoSession)

	assert.Equal(t, "base", awsdevops.SSOProfile(context.Background(), "app"))
}

func TestSSOProfileIgnoresRoleWithStaticSource(t *testing.T) {
	writeConfig(t, "[profile app]\nrole_arn = arn:aws:iam::123456789012:role/App\nsource_profile = base\n\n"+
		"[profile base]\naws_access_key_id = AKIAEXAMPLE\naws_secret_access_key = secret\n")

	assert.Empty(t, awsdevops.SSOProfile(context.Background(), "app"))
}

func TestSSOProfileReadsTheSelectedProfile(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session = corp\n"+ssoSession)
	t.Setenv("AWS_PROFILE", "dev")

	assert.Equal(t, "dev", awsdevops.SSOProfile(context.Background(), ""))
}
