package setup

import (
	"bytes"
	"encoding/base64"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/setup"
)

const snippet = "const LaunchDarkly = require('@launchdarkly/node-server-sdk');\nconst ldClient = LaunchDarkly.init('sdk-key');"

// copyKey sends "c" and runs whatever command Update returns, returning the updated
// model and everything written to the clipboard writer.
func copyKey(t *testing.T, m wizardModel) (wizardModel, string) {
	t.Helper()
	var out bytes.Buffer
	m.clipboard = &out

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd != nil {
		cmd()
	}
	return next.(wizardModel), out.String()
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

	updated, written := copyKey(t, m)

	require.NotEmpty(t, written, "pressing c must write an OSC 52 sequence")
	assert.Equal(t, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte(snippet))+"\x07", written)
	assert.True(t, updated.copied)

	decoded := decodeOSC52(t, written)
	assert.Equal(t, snippet, decoded)
	assert.NotContains(t, decoded, "│", "the gutter bar must not be copied")
	assert.NotContains(t, decoded, "  \n", "trailing padding must not be copied")
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

	_, written := copyKey(t, m)
	assert.Equal(t, snippet, decodeOSC52(t, written))
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

	_, written := copyKey(t, m)
	assert.Equal(t, "npm install @launchdarkly/node-server-sdk", decodeOSC52(t, written))
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

			assert.Empty(t, written, "must not write to the terminal with nothing to copy")
			assert.False(t, updated.copied)
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
	assert.False(t, typed.copied)
}

func decodeOSC52(t *testing.T, seq string) string {
	t.Helper()
	require.True(t, len(seq) > len("\x1b]52;c;")+1, "not an OSC 52 sequence: %q", seq)
	payload := seq[len("\x1b]52;c;") : len(seq)-1]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	return string(decoded)
}
