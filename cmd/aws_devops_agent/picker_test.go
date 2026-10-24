package awsdevopsagent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// pickerConfig has two profiles that share the signed-in corp session, and a
// profile with no SSO session.
const pickerConfig = "[profile static]\nregion = us-east-1\n\n" +
	"[profile dev]\nsso_session = corp\nsso_account_id = 111111111111\nsso_role_name = Admin\nregion = us-west-2\n\n" +
	"[profile prod]\nsso_session = corp\nsso_account_id = 222222222222\nsso_role_name = Viewer\nregion = us-east-1\n"

type chooseCall struct {
	title  string
	labels []string
}

// pickerTest gives l the config, a token cache in which the corp session is
// signed in, and a chooser that answers with the given indexes in order.
func pickerTest(t *testing.T, l ssoLogin, config string, answers ...int) (ssoLogin, *[]chooseCall) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	dir := t.TempDir()
	l.configPath = filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(l.configPath, []byte(config), 0o600))
	t.Setenv("AWS_CONFIG_FILE", l.configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	token := filepath.Join(dir, "token.json")
	require.NoError(t, os.WriteFile(token, []byte(`{"accessToken":"x","expiresAt":"2026-10-23T20:00:00Z"}`), 0o600))
	l.tokenPath = func(key string) (string, error) {
		if key == "corp" {
			return token, nil
		}

		return filepath.Join(dir, "missing.json"), nil
	}
	l.now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }

	var calls []chooseCall
	l.choose = func(title, _ string, labels []string) (int, error) {
		calls = append(calls, chooseCall{title: title, labels: labels})
		if len(calls) > len(answers) {
			return 0, errPickCanceled
		}

		return answers[len(calls)-1], nil
	}

	return l, &calls
}

// byProfile acts like NewClients when only the named profiles work. A profile
// without a region gets ErrNoRegion until a region is passed.
func byProfile(working map[string]string) func(context.Context, string, string) (awsdevops.Clients, error) {
	return func(_ context.Context, region, profile string) (awsdevops.Clients, error) {
		profileRegion, ok := working[profile]
		if !ok {
			return awsdevops.Clients{}, fmt.Errorf("wrapped: %w", awsdevops.ErrNoCredentials)
		}
		if region == "" {
			region = profileRegion
		}
		if region == "" {
			return awsdevops.Clients{}, awsdevops.ErrNoRegion
		}

		return awsdevops.Clients{Region: region}, nil
	}
}

func TestLoadAsksForAProfileWhenTheDefaultFails(t *testing.T) {
	f, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig, 1)
	l.newClients = byProfile(map[string]string{"prod": "us-east-1"})
	var picked string

	clients, err := l.load(context.Background(), "", "", func(name string) { picked = name })

	require.NoError(t, err)
	assert.Equal(t, "prod", picked)
	assert.Equal(t, "us-east-1", clients.Region)
	require.Len(t, *calls, 1)
	assert.Equal(t, []string{
		"✓ dev     111111111111  us-west-2  Admin",
		"✓ prod    222222222222  us-east-1  Viewer",
		"  static                us-east-1",
	}, (*calls)[0].labels)
	assert.Contains(t, f.prompted.String(), "Using AWS profile prod. To skip this question, pass --profile prod")
}

// Without a default profile, NewClients reports the region first. The
// profile can give the region, so load asks for the profile.
func TestLoadAsksForAProfileWhenNoRegionAndNoDefault(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig, 0)
	l.newClients = byProfile(map[string]string{"": "", "dev": "us-west-2"})

	clients, err := l.load(context.Background(), "", "", func(string) {})

	require.NoError(t, err)
	assert.Equal(t, "us-west-2", clients.Region)
	require.Len(t, *calls, 1)
	assert.Equal(t, "Which AWS profile should ldcli use?", (*calls)[0].title)
}

func TestLoadAsksForARegionWhenTheProfileHasNone(t *testing.T) {
	f, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig, 2, 1)
	l.newClients = byProfile(map[string]string{"static": ""})

	clients, err := l.load(context.Background(), "", "", func(string) {})

	require.NoError(t, err)
	assert.Equal(t, awsdevops.SupportedRegions[1], clients.Region)
	require.Len(t, *calls, 2)
	assert.Equal(t, "Which AWS region should ldcli use?", (*calls)[1].title)
	assert.Equal(t, awsdevops.SupportedRegions, (*calls)[1].labels)
	assert.Contains(t, f.prompted.String(), "pass --region "+awsdevops.SupportedRegions[1])
}

func TestLoadKeepsAWorkingDefault(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, "[default]\nregion = us-east-1\n\n"+pickerConfig)
	l.newClients = byProfile(map[string]string{"": "us-east-1"})

	_, err := l.load(context.Background(), "", "", func(string) { t.Fatal("picked a profile") })

	require.NoError(t, err)
	assert.Empty(t, *calls)
}

func TestLoadDoesNotAskWithASelectedProfile(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig)
	l.newClients = byProfile(map[string]string{})

	_, err := l.load(context.Background(), "", "prod", func(string) { t.Fatal("picked a profile") })

	require.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.Empty(t, *calls)
}

func TestLoadWithoutATerminalReturnsTheError(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig)
	l.tty = func() bool { return false }
	l.newClients = byProfile(map[string]string{})

	_, err := l.load(context.Background(), "", "", func(string) { t.Fatal("picked a profile") })

	require.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.Empty(t, *calls)
}

func TestLoadWithYesUsesTheOneSignedInProfile(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, "[profile static]\nregion = us-east-1\n\n[profile dev]\nsso_session = corp\nregion = us-west-2\n")
	l.yes = true
	l.newClients = byProfile(map[string]string{"dev": "us-west-2"})
	var picked string

	_, err := l.load(context.Background(), "", "", func(name string) { picked = name })

	require.NoError(t, err)
	assert.Equal(t, "dev", picked)
	assert.Empty(t, *calls)
}

func TestLoadWithYesAndSeveralSignedInProfilesReturnsTheError(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig)
	l.yes = true
	l.newClients = byProfile(map[string]string{})

	_, err := l.load(context.Background(), "", "", func(string) { t.Fatal("picked a profile") })

	require.ErrorIs(t, err, awsdevops.ErrNoCredentials)
	assert.Empty(t, *calls)
}

func TestLoadReportsAClosedPicker(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, _ = pickerTest(t, l, pickerConfig)
	l.newClients = byProfile(map[string]string{})

	_, err := l.load(context.Background(), "", "", func(string) { t.Fatal("picked a profile") })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no AWS profile was chosen")
}

// The real picker searches with / and picks the first match.
func TestHuhChooserSearchesBySubstring(t *testing.T) {
	labels := profileLabels([]awsdevops.Profile{
		{Name: "dev", Account: "111111111111", Role: "Admin"},
		{Name: "prod", Account: "222222222222", Role: "Viewer"},
		{Name: "staging", Account: "333333333333", Role: "Admin"},
	})
	choose := huhChooser(iotest.OneByteReader(strings.NewReader("/2222\r\r")), &bytes.Buffer{})

	choice, err := choose("Which AWS profile should ldcli use?", "Press / to search.", labels)

	require.NoError(t, err)
	assert.Equal(t, 1, choice)
}

// A long list scrolls in place, so the picker shows pickerRows options.
func TestPickerFieldShowsAFixedNumberOfRows(t *testing.T) {
	labels := make([]string, 40)
	for i := range labels {
		labels[i] = fmt.Sprintf("profile-%02d", i)
	}
	selected := 0

	view := pickerField("Which AWS profile should ldcli use?", "Press / to search.", labels, &selected).View()

	assert.Contains(t, view, fmt.Sprintf("profile-%02d", pickerRows-1))
	assert.NotContains(t, view, fmt.Sprintf("profile-%02d", pickerRows))
}

func TestPickerFieldFitsAShortList(t *testing.T) {
	selected := 0

	view := pickerField("Which AWS region should ldcli use?", "", []string{"us-east-1", "us-west-2"}, &selected).View()

	assert.Contains(t, view, "us-west-2")
	assert.LessOrEqual(t, strings.Count(view, "\n"), 3)
}
