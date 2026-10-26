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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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
	l.choose = func(title, _ string, labels []string, _ int) (int, error) {
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

// NewClients reports a missing region before missing credentials. A default
// profile with no credentials and no region, for example one with only an
// output key, gets the profile picker.
func TestLoadAsksForAProfileWhenTheDefaultHasNoRegionAndNoCredentials(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig, 0)
	working := byProfile(map[string]string{"dev": "us-west-2"})
	l.newClients = func(ctx context.Context, region, profile string) (awsdevops.Clients, error) {
		if profile == "" && region == "" {
			return awsdevops.Clients{}, awsdevops.ErrNoRegion
		}

		return working(ctx, region, profile)
	}

	clients, err := l.load(context.Background(), "", "", func(string) {})

	require.NoError(t, err)
	assert.Equal(t, "us-west-2", clients.Region)
	require.Len(t, *calls, 1)
	assert.Equal(t, "Which AWS profile should ldcli use?", (*calls)[0].title)
}

// Default credentials that work without a region get the region picker.
func TestLoadAsksForARegionWhenTheDefaultHasNone(t *testing.T) {
	_, l := newLoginTest(t, "")
	l, calls := pickerTest(t, l, pickerConfig, 1)
	l.newClients = byProfile(map[string]string{"": ""})

	clients, err := l.load(context.Background(), "", "", func(string) { t.Fatal("picked a profile") })

	require.NoError(t, err)
	assert.Equal(t, awsdevops.SupportedRegions[1], clients.Region)
	require.Len(t, *calls, 1)
	assert.Equal(t, "Which AWS region should ldcli use?", (*calls)[0].title)
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

func numbered(n int) []string {
	labels := make([]string, n)
	for i := range labels {
		labels[i] = fmt.Sprintf("profile-%02d", i)
	}

	return labels
}

func press(p picker, keys ...tea.KeyMsg) picker {
	for _, k := range keys {
		model, _ := p.Update(k)
		p = model.(picker)
	}

	return p
}

func typed(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

var down = tea.KeyMsg{Type: tea.KeyDown}

// rows returns the list rows on the screen, and the highlighted one.
func rows(p picker) (shown []string, highlighted string) {
	for _, line := range strings.Split(p.View(), "\n") {
		line = ansi.Strip(line)
		switch {
		case strings.HasPrefix(line, "> "):
			highlighted = strings.TrimPrefix(line, "> ")
			shown = append(shown, highlighted)
		case strings.HasPrefix(line, "  profile-"):
			shown = append(shown, strings.TrimPrefix(line, "  "))
		}
	}

	return shown, highlighted
}

func TestPickerShowsAllRowsOfAShortList(t *testing.T) {
	p := newPicker("Pick", "", numbered(pickerRows), 0)

	shown, highlighted := rows(p)

	assert.Equal(t, numbered(pickerRows), shown)
	assert.Equal(t, "profile-00", highlighted)
}

// The highlight moves down to the last row. Then the list scrolls, and the
// highlight stays on the last row.
func TestPickerHighlightMovesDownAndStaysOnTheLastRow(t *testing.T) {
	p := newPicker("Pick", "", numbered(40), 0)

	p = press(p, down, down, down)
	shown, highlighted := rows(p)
	assert.Equal(t, numbered(pickerRows), shown)
	assert.Equal(t, "profile-03", highlighted)

	for range pickerRows {
		p = press(p, down)
	}
	shown, highlighted = rows(p)
	assert.Len(t, shown, pickerRows)
	assert.Equal(t, "profile-13", highlighted)
	assert.Equal(t, "profile-13", shown[pickerRows-1])
	assert.Equal(t, "profile-04", shown[0])
}

// Moving up past the first row scrolls back, with the highlight on top.
func TestPickerScrollsBackUp(t *testing.T) {
	p := newPicker("Pick", "", numbered(40), 0)
	for range 15 {
		p = press(p, down)
	}

	for range pickerRows {
		p = press(p, tea.KeyMsg{Type: tea.KeyUp})
	}
	shown, highlighted := rows(p)

	assert.Equal(t, "profile-05", highlighted)
	assert.Equal(t, "profile-05", shown[0])
}

func TestPickerStopsAtTheEnds(t *testing.T) {
	p := newPicker("Pick", "", numbered(3), 0)

	p = press(p, tea.KeyMsg{Type: tea.KeyUp})
	_, highlighted := rows(p)
	assert.Equal(t, "profile-00", highlighted)

	p = press(p, down, down, down, down)
	_, highlighted = rows(p)
	assert.Equal(t, "profile-02", highlighted)
}

func TestPickerSearchesBySubstringAndShowsAllMatches(t *testing.T) {
	p := newPicker("Pick", "", numbered(40), 0)

	p = press(p, typed("1"))
	shown, highlighted := rows(p)

	// 01, 10 to 19, 21, and 31 contain "1".
	assert.Len(t, shown, pickerRows)
	assert.Equal(t, "profile-01", highlighted)
	assert.Contains(t, ansi.Strip(p.View()), "13 of 40")

	p = press(p, typed("7"))
	shown, highlighted = rows(p)
	assert.Equal(t, []string{"profile-17"}, shown)
	assert.Equal(t, "profile-17", highlighted)
}

func TestPickerSearchIgnoresCase(t *testing.T) {
	p := newPicker("Pick", "", []string{"dev  Admin", "prod  Viewer"}, 0)

	p = press(p, typed("VIEW"), tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, p.done)
	assert.Equal(t, 1, p.chosen)
}

func TestPickerEnterDoesNothingWithNoMatch(t *testing.T) {
	p := newPicker("Pick", "", numbered(3), 0)

	p = press(p, typed("zzz"), tea.KeyMsg{Type: tea.KeyEnter})

	assert.False(t, p.done)
	assert.Contains(t, p.View(), "No match")
}

func TestPickerEscClearsTheSearchAndThenCancels(t *testing.T) {
	p := newPicker("Pick", "", numbered(3), 0)
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	p = press(p, typed("02"), esc)
	shown, _ := rows(p)
	assert.Len(t, shown, 3)
	assert.False(t, p.done)

	p = press(p, esc)
	assert.True(t, p.canceled)
}

func TestPickerCutsLinesToTheTerminalWidth(t *testing.T) {
	p := newPicker("Pick", "", []string{strings.Repeat("x", 200)}, 0)
	model, _ := p.Update(tea.WindowSizeMsg{Width: 40, Height: 20})

	for _, line := range strings.Split(model.View(), "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 40)
	}
}

// The real program reads the keys from the input.
func TestTeaChooserPicksASearchedRow(t *testing.T) {
	labels := profileLabels([]awsdevops.Profile{
		{Name: "dev", Account: "111111111111", Role: "Admin"},
		{Name: "prod", Account: "222222222222", Role: "Viewer"},
		{Name: "staging", Account: "333333333333", Role: "Admin"},
	})
	choose := teaChooser(iotest.OneByteReader(strings.NewReader("2222\r")), &bytes.Buffer{})

	choice, err := choose("Which AWS profile should ldcli use?", "Type to search.", labels, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, choice)
}

func TestTeaChooserReportsCtrlC(t *testing.T) {
	choose := teaChooser(strings.NewReader("\x03"), &bytes.Buffer{})

	_, err := choose("Pick", "", numbered(3), 0)

	require.ErrorIs(t, err, errPickCanceled)
}

func TestPickerStartsOnTheStartRow(t *testing.T) {
	p := newPicker("Pick", "", numbered(40), 15)

	shown, highlighted := rows(p)

	assert.Equal(t, "profile-15", highlighted)
	assert.Equal(t, "profile-15", shown[len(shown)-1])
}

// A wide character takes two columns, so the columns still line up.
func TestProfileLabelsAlignWideCharacters(t *testing.T) {
	labels := profileLabels([]awsdevops.Profile{
		{Name: "開発", Account: "111111111111", Role: "Admin"},
		{Name: "prod", Account: "222222222222", Role: "Viewer"},
	})

	column := func(label, account string) int { return ansi.StringWidth(label[:strings.Index(label, account)]) }
	assert.Equal(t, column(labels[1], "222"), column(labels[0], "111"))
}
