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
			cause := errors.New(imdsCause)

			err := noCredentialsError(tc.selected, cause)

			assert.NotContains(t, err.Error(), "169.254.169.254")
			assert.NotContains(t, err.Error(), "attempts")
			assert.ErrorIs(t, err, ErrNoCredentials)
			assert.Same(t, cause, errors.Unwrap(err))
		})
	}
}

func TestCredentialErrorNamesTheProfileAndLogin(t *testing.T) {
	writeProfiles(t, "dev")

	err := noCredentialsError("dev", errors.New(imdsCause))

	assert.Contains(t, err.Error(), `"dev"`)
	assert.Contains(t, err.Error(), "aws sso login --profile dev")
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
		msg := noCredentialsError(profile, errors.New(imdsCause)).Error()
		assert.NotContains(t, msg, ";")
		assert.NotContains(t, msg, "—")
	}
}
