package awsdevops_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestHasSSOConfigFindsSSOSession(t *testing.T) {
	path := writeConfig(t, "[profile dev]\nsso_session = corp\nregion = us-east-1\n")

	assert.True(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigFindsLegacyStartURL(t *testing.T) {
	path := writeConfig(t, "[profile dev]\nsso_start_url=https://corp.awsapps.com/start\n")

	assert.True(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigReadsDefaultSection(t *testing.T) {
	path := writeConfig(t, "[default]\nsso_session = corp\n")

	assert.True(t, awsdevops.HasSSOConfig(path, "default"))
}

func TestHasSSOConfigIgnoresNearMissProfile(t *testing.T) {
	path := writeConfig(t, "[profile dev]\nregion = us-east-1\n\n[profile dev-admin]\nsso_session = corp\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigIgnoresOtherProfileBeforeIt(t *testing.T) {
	path := writeConfig(t, "[profile dev-admin]\nsso_session = corp\n\n[profile dev]\nregion = us-east-1\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigIgnoresSSORegionOnly(t *testing.T) {
	path := writeConfig(t, "[profile dev]\nsso_region = us-east-1\nsso_account_id = 123\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigIgnoresCommentedLines(t *testing.T) {
	path := writeConfig(t, "[profile dev]\n# sso_start_url = https://corp.awsapps.com/start\n; sso_session = corp\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigIgnoresNearMissKey(t *testing.T) {
	path := writeConfig(t, "[profile dev]\nsso_session_name = corp\nmy_sso_start_url = x\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigIgnoresSessionSection(t *testing.T) {
	path := writeConfig(t, "[sso-session dev]\nsso_start_url = https://corp.awsapps.com/start\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigIgnoresBareDefaultForOtherName(t *testing.T) {
	path := writeConfig(t, "[default]\nsso_session = corp\n")

	assert.False(t, awsdevops.HasSSOConfig(path, "dev"))
}

func TestHasSSOConfigHandlesMissingFile(t *testing.T) {
	assert.False(t, awsdevops.HasSSOConfig(filepath.Join(t.TempDir(), "absent"), "dev"))
}

func TestConfigFilePathUsesEnvOverride(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/elsewhere/aws-config")

	assert.Equal(t, "/elsewhere/aws-config", awsdevops.ConfigFilePath())
}

func TestConfigFilePathDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AWS_CONFIG_FILE", "")

	assert.Equal(t, filepath.Join(home, ".aws", "config"), awsdevops.ConfigFilePath())
}
