package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
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
	t.Setenv("AWS_DEFAULT_PROFILE", "")
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
	t.Setenv("AWS_DEFAULT_PROFILE", "")
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

// expiredSSO acts like the SDK error for an expired SSO token.
func expiredSSO() error {
	return fmt.Errorf("failed to refresh cached credentials, %w", &ssocreds.InvalidTokenError{})
}

func TestCredentialErrorNamesTheProfileAndLogin(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session = corp\n")

	err := noCredentialsError("dev", expiredSSO())

	assert.Contains(t, err.Error(), `"dev"`)
	assert.Contains(t, err.Error(), "aws sso login --profile dev")
}

func TestCredentialErrorNamesTheDefaultSSOProfile(t *testing.T) {
	writeConfig(t, "[default]\nsso_session = corp\n")

	err := noCredentialsError("", expiredSSO())

	assert.Contains(t, err.Error(), "aws sso login --profile default")
}

func TestCredentialErrorReadsAWSDefaultProfile(t *testing.T) {
	writeConfig(t, "[profile corp]\nsso_session = corp\n")
	t.Setenv("AWS_DEFAULT_PROFILE", "corp")

	err := noCredentialsError("", expiredSSO())

	assert.Contains(t, err.Error(), "aws sso login --profile corp")
}

// A login does not fix an SSO error with another cause.
func TestCredentialErrorPointsToTheCauseOfOtherSSOErrors(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session = corp\n")
	cause := errors.New("ForbiddenException: No access")

	err := noCredentialsError("dev", cause)

	assert.Contains(t, err.Error(), `profile "dev"`)
	assert.Contains(t, err.Error(), "aws sts get-caller-identity --profile dev")
	assert.NotContains(t, err.Error(), "No access")
	assert.NotContains(t, err.Error(), "sso login")
}

// The SDK uses the default profile without --profile or AWS_PROFILE.
func TestCredentialErrorPointsToTheCauseForTheDefaultProfile(t *testing.T) {
	writeConfig(t, "[default]\ncredential_process = /usr/local/bin/get-creds\n")
	cause := errors.New("process provider error: exit status 1")

	err := noCredentialsError("", cause)

	assert.Contains(t, err.Error(), `profile "default"`)
	assert.Contains(t, err.Error(), "aws sts get-caller-identity --profile default")
	assert.NotContains(t, err.Error(), "exit status 1")
	assert.NotContains(t, err.Error(), "no AWS profile selected")
}

// Only SSO profiles get the SSO advice.
func TestCredentialErrorPointsToTheCauseForOtherProfiles(t *testing.T) {
	writeConfig(t, "[profile ci]\ncredential_process = /usr/local/bin/get-creds\n")
	cause := errors.New("process provider error: exit status 1")

	err := noCredentialsError("ci", cause)

	assert.Contains(t, err.Error(), `profile "ci"`)
	assert.Contains(t, err.Error(), "aws sts get-caller-identity --profile ci")
	assert.NotContains(t, err.Error(), "exit status 1")
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

// writeSSOConfig writes an sso_session profile, the format that aws configure
// sso writes, and a legacy sso_start_url profile. HOME holds the token cache.
func writeSSOConfig(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	writeConfig(t, `[profile dev]
sso_session = corp
sso_account_id = 123456789012
sso_role_name = Admin
region = us-east-1

[sso-session corp]
sso_start_url = https://example.awsapps.com/start
sso_region = us-east-1

[profile legacy]
sso_start_url = https://example.awsapps.com/start
sso_region = us-east-1
sso_account_id = 123456789012
sso_role_name = Admin
region = us-east-1
`)
}

// These tests use the real SDK, so they find the errors it really returns.
func TestSSOTokenInvalidForEachSDKCase(t *testing.T) {
	cases := map[string]struct {
		profile string
		cache   func(t *testing.T)
	}{
		"sso_session without a cached token": {profile: "dev"},
		"sso_session with an expired token": {profile: "dev", cache: func(t *testing.T) {
			path, err := ssocreds.StandardCachedTokenFilepath("corp")
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte(`{"accessToken":"old","expiresAt":"2020-01-01T00:00:00Z"}`), 0o600))
		}},
		"legacy sso_start_url without a cached token": {profile: "legacy"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			writeSSOConfig(t)
			if tc.cache != nil {
				tc.cache(t)
			}

			_, err := NewClients(context.Background(), "us-east-1", tc.profile)

			require.ErrorIs(t, err, ErrNoCredentials)
			assert.True(t, SSOTokenInvalid(err), "cause: %v", errors.Unwrap(err))
			assert.Contains(t, err.Error(), "aws sso login --profile "+tc.profile)
		})
	}
}

func TestSSOTokenInvalidIgnoresOtherErrors(t *testing.T) {
	assert.False(t, SSOTokenInvalid(nil))
	assert.False(t, SSOTokenInvalid(errors.New("ForbiddenException: No access")))
}

// A credential_process that prints a line before the JSON makes the SDK copy
// all of its output, with the secret key, into the cause.
func TestCredentialErrorHidesTheProcessOutput(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "get-creds")
	require.NoError(t, os.WriteFile(helper, []byte("#!/bin/sh\n"+
		"echo 'warning: token cache refreshed'\n"+
		`echo '{"Version":1,"AccessKeyId":"AKIAFAKEEXAMPLE","SecretAccessKey":"FAKE-SECRET-DO-NOT-USE"}'`+"\n"), 0o700))
	writeConfig(t, "[profile leak]\nregion = us-east-1\ncredential_process = "+helper+"\n")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	_, err := NewClients(context.Background(), "", "leak")

	require.ErrorIs(t, err, ErrNoCredentials)
	assert.NotContains(t, err.Error(), "FAKE-SECRET-DO-NOT-USE")
	assert.NotContains(t, err.Error(), "AKIAFAKEEXAMPLE")
	assert.Contains(t, err.Error(), "aws sts get-caller-identity --profile leak")
}

// Without a profile name, the hint uses the default credential chain.
func TestCredentialErrorPointsToTheCauseWithoutAProfile(t *testing.T) {
	writeProfiles(t)

	err := noCredentialsError("", errors.New("process provider error: exit status 1"))

	assert.Equal(t, "unable to get AWS credentials. Run `aws sts get-caller-identity` to see the cause", err.Error())
}

// An expired default profile from aws login hides a profile from aws sso
// login, so the error tells how to pick that profile.
func TestCredentialErrorNamesOtherProfilesWhenTheDefaultFails(t *testing.T) {
	writeConfig(t, "[default]\nlogin_session = arn:aws:iam::123456789012:user/me\n\n"+
		"[profile dev]\nsso_start_url = https://corp.awsapps.com/start\n")

	err := noCredentialsError("", errors.New("login session has expired, please reauthenticate"))

	assert.Contains(t, err.Error(), "aws sts get-caller-identity --profile default")
	assert.Contains(t, err.Error(), "To use another profile, pass --profile <name> or run `export AWS_PROFILE=<name>`. Profiles: dev")
	assert.NotContains(t, err.Error(), "Profiles: default")
}

func TestCredentialErrorDoesNotNameOtherProfilesForASelectedProfile(t *testing.T) {
	writeConfig(t, "[profile dev]\nsso_session = corp\n\n[profile prod]\nsso_session = corp\n")

	err := noCredentialsError("dev", expiredSSO())

	assert.Contains(t, err.Error(), "aws sso login --profile dev")
	assert.NotContains(t, err.Error(), "To use another profile")
}

func TestNoProfileSelected(t *testing.T) {
	writeConfig(t, "[profile dev]\nregion = us-east-1\n")
	t.Setenv("AWS_ACCESS_KEY_ID", "")

	assert.True(t, NoProfileSelected(""))
	assert.False(t, NoProfileSelected("dev"))

	t.Setenv("AWS_PROFILE", "dev")
	assert.False(t, NoProfileSelected(""))

	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIA")
	assert.False(t, NoProfileSelected(""))

	t.Setenv("AWS_ACCESS_KEY_ID", "")
	writeConfig(t, "[default]\nregion = us-east-1\n")
	assert.False(t, NoProfileSelected(""))
}
