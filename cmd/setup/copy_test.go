package setup

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/setup"
)

const snippet = "const LaunchDarkly = require('@launchdarkly/node-server-sdk');\nconst ldClient = LaunchDarkly.init('sdk-key');"

// copyKey sends "c" with a working OS clipboard, and returns the updated model
// alongside what each path received.
func copyKey(t *testing.T, m wizardModel) (wizardModel, string) {
	t.Helper()
	var native string
	updated, terminal := copyKeyWith(t, m, func(s string) error {
		native = s
		return nil
	})
	return updated, native + terminal
}

// copyKeyWith sends "c" with the given OS clipboard behaviour, and returns the
// updated model and whatever was written to the terminal as an OSC 52 sequence.
func copyKeyWith(t *testing.T, m wizardModel, native func(string) error) (wizardModel, string) {
	t.Helper()
	var out bytes.Buffer
	m.clipboard = &out
	m.nativeCopy = native

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(wizardModel)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ = m.Update(msg)
			m = next.(wizardModel)
		}
	}
	return m, out.String()
}

// The snippet has to arrive on the clipboard exactly as the user needs to paste it:
// the gutter bar the code block is drawn with, and the padding lipgloss adds to square
// it off, are display only and must not be copied.
func TestWizard_CopySnippet_CopiesRawContent(t *testing.T) {
	m := wizardModel{
		step:  stepDone,
		width: 80,
		initResult: &setup.InitResult{
			SDKID:    "go-server-sdk",
			FilePath: "/proj/main.go",
			Snippet:  snippet,
			Success:  false,
		},
	}

	// The rendered block carries the decoration the raw copy must not.
	require.Contains(t, m.View(), "│", "the code block is drawn with a gutter bar")

	var native string
	updated, _ := copyKeyWith(t, m, func(s string) error {
		native = s
		return nil
	})

	assert.Equal(t, snippet, native)
	assert.Equal(t, copyDone, updated.copyState)
	assert.NotContains(t, native, "│", "the gutter bar must not be copied")
	assert.NotContains(t, native, "  \n", "trailing padding must not be copied")
}

// A screen can show both an install command and a snippet. The snippet is the one
// that has to be pasted verbatim, so that is what c copies.
func TestWizard_CopySnippet_PrefersSnippetOverInstallCommand(t *testing.T) {
	m := wizardModel{
		step:  stepDone,
		width: 80,
		installResult: &setup.InstallResult{
			Command: "npm install @launchdarkly/node-server-sdk",
			Failed:  true,
		},
		initResult: &setup.InitResult{
			SDKID:    "node-server",
			FilePath: "/proj/index.js",
			Snippet:  snippet,
			Success:  false,
		},
	}

	_, copied := copyKey(t, m)
	assert.Equal(t, snippet, copied)
	assert.Contains(t, m.View(), "Press c to copy the snippet.")
}

// With no snippet to paste, the thing the user still has to carry out of the wizard
// is the install command.
func TestWizard_CopySnippet_FallsBackToInstallCommand(t *testing.T) {
	m := wizardModel{
		step:  stepDone,
		width: 80,
		installResult: &setup.InstallResult{
			Command: "npm install @launchdarkly/node-server-sdk",
			Failed:  true,
		},
		initResult: &setup.InitResult{SDKID: "node-server", FilePath: "/proj/index.js", Success: true},
	}

	_, copied := copyKey(t, m)
	assert.Equal(t, "npm install @launchdarkly/node-server-sdk", copied)
	assert.Contains(t, m.View(), "Press c to copy the command.")
}

// Offering a copy on a screen with nothing to copy, or writing to the terminal on a
// key the screen does not handle, would both be wrong.
func TestWizard_CopySnippet_NothingToCopy(t *testing.T) {
	tests := []struct {
		name string
		m    wizardModel
	}{
		{
			name: "verification succeeded, no manual step left",
			m: wizardModel{
				step:         stepDone,
				width:        80,
				initResult:   &setup.InitResult{SDKID: "node-server", Success: true},
				verifyResult: &setup.VerifyResult{Active: true},
				detectResult: &setup.DetectResult{SDKID: "node-server"},
			},
		},
		{
			name: "mid-flow screen shows no code",
			m:    wizardModel{step: stepSelectSDK, width: 80},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, written := copyKey(t, tt.m)

			assert.Empty(t, written, "must not copy anything with nothing to copy")
			assert.Equal(t, copyNone, updated.copyState)
			assert.NotContains(t, tt.m.View(), "Press c to copy")
		})
	}
}

// The hint has to confirm the copy, otherwise the user has no way to tell whether the
// key did anything.
func TestWizard_CopySnippet_HintConfirmsAfterCopying(t *testing.T) {
	m := wizardModel{
		step:  stepDone,
		width: 80,
		initResult: &setup.InitResult{
			SDKID:    "go-server-sdk",
			FilePath: "/proj/main.go",
			Snippet:  snippet,
			Success:  false,
		},
	}

	assert.Contains(t, m.View(), "Press c to copy the snippet.")

	updated, _ := copyKey(t, m)
	view := updated.View()
	assert.Contains(t, view, "Copied the snippet to your clipboard.")
	assert.NotContains(t, view, "Press c to copy")
}

// 'c' is a legal character in a filter query, so the list has to keep receiving it.
func TestWizard_CopySnippet_DoesNotStealCFromFiltering(t *testing.T) {
	m := wizardModel{step: stepDetect, width: 80, height: 30}
	next, _ := m.Update(detectDoneMsg{result: &setup.DetectResult{
		SDKID:    "node-server",
		Language: "JavaScript",
	}})
	m2 := next.(wizardModel)
	m2.sdkFocus = 1

	// Open the list filter, then type "c".
	filtering, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m3 := filtering.(wizardModel)
	require.True(t, m3.isFiltering(), "expected the SDK list to be filtering")

	typed, written := copyKey(t, m3)
	assert.Empty(t, written, "c must reach the filter, not the clipboard")
	assert.Equal(t, copyNone, typed.copyState)
}

// Over SSH the OS clipboard belongs to the wrong machine, so a failure there falls
// back to asking the terminal. That path cannot be confirmed, so the hint must not
// claim the content is on the clipboard.
func TestWizard_CopySnippet_FallsBackToTerminalWhenOSClipboardFails(t *testing.T) {
	m := wizardModel{
		step:  stepDone,
		width: 80,
		initResult: &setup.InitResult{
			SDKID:    "go-server-sdk",
			FilePath: "/proj/main.go",
			Snippet:  snippet,
			Success:  false,
		},
	}

	updated, written := copyKeyWith(t, m, func(string) error {
		return errors.New("no clipboard on this machine")
	})

	require.NotEmpty(t, written, "a failed OS copy must fall back to OSC 52")
	assert.Equal(t, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte(snippet))+"\x07", written)
	assert.Equal(t, snippet, decodeOSC52(t, written))
	assert.Equal(t, copyRequested, updated.copyState)

	view := updated.View()
	assert.Contains(t, view, "Asked your terminal to copy the snippet.")
	assert.NotContains(t, view, "Copied the snippet to your clipboard.",
		"OSC 52 support cannot be detected, so the copy must not be claimed as done")
}

// A remote host can have a perfectly working clipboard — a Mac with pbcopy, a Linux
// box with a display — and writing to it still puts the snippet on the wrong machine.
// Success there is not evidence the user can paste, so it must not be preferred or
// reported as done.
func TestWizard_CopySnippet_RemoteSessionSkipsTheOSClipboard(t *testing.T) {
	m := wizardModel{
		step:          stepDone,
		width:         80,
		remoteSession: true,
		initResult: &setup.InitResult{
			SDKID:    "go-server-sdk",
			FilePath: "/proj/main.go",
			Snippet:  snippet,
			Success:  false,
		},
	}

	nativeCalled := false
	updated, written := copyKeyWith(t, m, func(string) error {
		nativeCalled = true
		return nil // the remote clipboard would accept it
	})

	assert.False(t, nativeCalled, "must not write to the clipboard of the remote machine")
	assert.Equal(t, snippet, decodeOSC52(t, written))
	assert.Equal(t, copyRequested, updated.copyState)
	assert.Contains(t, updated.View(), "Asked your terminal to copy the snippet.")
}

// The environment sshd sets for its session is what separates the two cases.
func TestIsRemoteSession(t *testing.T) {
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "10.0.0.1 51234 10.0.0.2 22")
			assert.True(t, isRemoteSession())
		})
	}

	t.Run("no ssh variables", func(t *testing.T) {
		t.Setenv("SSH_CONNECTION", "")
		t.Setenv("SSH_CLIENT", "")
		t.Setenv("SSH_TTY", "")
		assert.False(t, isRemoteSession())
	})
}

func decodeOSC52(t *testing.T, seq string) string {
	t.Helper()
	require.True(t, len(seq) > len("\x1b]52;c;")+1, "not an OSC 52 sequence: %q", seq)
	payload := seq[len("\x1b]52;c;") : len(seq)-1]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	return string(decoded)
}
