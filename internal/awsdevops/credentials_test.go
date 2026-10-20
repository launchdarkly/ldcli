package awsdevops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const imdsCause = "operation error ec2imds: GetMetadata, exceeded maximum number of attempts, 3, " +
	"request send failed, Get \"http://169.254.169.254/latest/meta-data/iam/security-credentials/\""

// imdsErr acts like the SDK error from the EC2 metadata service.
type imdsErr struct{}

func (imdsErr) Error() string   { return imdsCause }
func (imdsErr) Service() string { return "ec2imds" }

func imdsFailure() error {
	return fmt.Errorf("failed to refresh cached credentials, no EC2 IMDS role found, %w", imdsErr{})
}

// writeConfig points the SDK at a temp config with this content.
func writeConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_PROFILE", "")
}

// writeProfiles points the SDK at a temp config.
func writeProfiles(t *testing.T, names ...string) {
	t.Helper()
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "[profile %s]\nregion = us-east-1\n\n", name)
	}
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	t.Setenv("AWS_PROFILE", "")
}

func TestCredentialErrorHidesTheSDKCause(t *testing.T) {
	cases := map[string]struct {
		profiles []string
		selected string
	}{
		"named profile":              {profiles: []string{"dev"}, selected: "dev"},
		"profiles but none selected": {profiles: []string{"dev", "prod"}},
		"no profiles":                {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			writeProfiles(t, tc.profiles...)
			cause := imdsFailure()

			err := noCredentialsError(tc.selected, cause)

			assert.NotContains(t, err.Error(), "169.254.169.254")
			assert.NotContains(t, err.Error(), "attempts")
			assert.ErrorIs(t, err, ErrNoCredentials)
			assert.Same(t, cause, errors.Unwrap(err))
		})
	}
}

func TestCredentialErrorNamesTheProfileAndLogin(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session = corp\n")

	err := noCredentialsError("dev", imdsFailure())

	assert.Contains(t, err.Error(), `"dev"`)
	assert.Contains(t, err.Error(), "aws sso login --profile dev")
}

func TestCredentialErrorNamesTheDefaultSSOProfile(t *testing.T) {
	writeConfig(t, "[default]\nsso_session = corp\n")

	err := noCredentialsError("", imdsFailure())

	assert.Contains(t, err.Error(), "aws sso login --profile default")
}

// Only SSO profiles get the SSO advice.
func TestCredentialErrorShowsTheCauseForOtherProfiles(t *testing.T) {
	writeConfig(t, "[profile ci]\ncredential_process = /usr/local/bin/get-creds\n")
	cause := errors.New("process provider error: exit status 1")

	err := noCredentialsError("ci", cause)

	assert.Contains(t, err.Error(), `profile "ci"`)
	assert.Contains(t, err.Error(), "exit status 1")
	assert.NotContains(t, err.Error(), "sso login")
	assert.ErrorIs(t, err, ErrNoCredentials)
}

func TestCredentialErrorSaysAProfileHasNoCredentials(t *testing.T) {
	writeProfiles(t, "dev")

	for name, cause := range map[string]error{"imds": imdsFailure(), "no keys": nil} {
		t.Run(name, func(t *testing.T) {
			err := noCredentialsError("dev", cause)

			assert.Contains(t, err.Error(), `profile "dev" has no AWS credentials`)
			assert.NotContains(t, err.Error(), "sso login")
			assert.NotContains(t, err.Error(), "169.254.169.254")
		})
	}
}

func TestCredentialErrorShowsFiveProfilesAtMost(t *testing.T) {
	var names []string
	for i := 0; i < 50; i++ {
		names = append(names, fmt.Sprintf("profile-%02d", i))
	}
	writeProfiles(t, names...)

	msg := noCredentialsError("", nil).Error()

	shown := 0
	for _, name := range names {
		if strings.Contains(msg, name) {
			shown++
		}
	}
	assert.Equal(t, 5, shown)
	assert.Contains(t, msg, "and 45 more")
	assert.Contains(t, msg, "aws configure list-profiles")
}

func TestCredentialErrorWithExactlyFiveProfiles(t *testing.T) {
	names := []string{"a1", "b2", "c3", "d4", "e5"}
	writeProfiles(t, names...)

	msg := noCredentialsError("", nil).Error()

	for _, name := range names {
		assert.Contains(t, msg, name)
	}
	assert.NotContains(t, msg, "more")
}

func TestCredentialErrorCountsPrefixProfilesSeparately(t *testing.T) {
	// dev is a prefix of dev-admin.
	writeProfiles(t, "a1", "b2", "c3", "dev-admin", "e5", "dev")

	msg := noCredentialsError("", nil).Error()

	assert.Contains(t, msg, "dev-admin")
	assert.Contains(t, msg, "and 1 more")
	assert.NotContains(t, msg, "dev,")
	assert.False(t, strings.HasSuffix(strings.SplitN(msg, "and 1 more", 2)[0], "dev "))
}

func TestCredentialErrorCountsDuplicateProfilesOnce(t *testing.T) {
	writeProfiles(t, "a1", "b2", "c3", "d4", "e5", "f6")
	creds := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(creds, []byte("[a1]\n[f6]\n"), 0o600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", creds)

	msg := noCredentialsError("", nil).Error()

	assert.Contains(t, msg, "and 1 more")
}

func TestCredentialErrorHasNoSemicolonOrDash(t *testing.T) {
	writeProfiles(t, "a1", "b2", "c3", "d4", "e5", "f6")
	for _, profile := range []string{"", "a1"} {
		msg := noCredentialsError(profile, imdsFailure()).Error()
		assert.NotContains(t, msg, ";")
		assert.NotContains(t, msg, "—")
	}
}
