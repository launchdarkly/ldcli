package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmApply(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		terminal  bool
		confirmed bool
		wantError string
	}{
		{name: "yes", input: "yes\n", terminal: true, confirmed: true},
		{name: "declined", input: "n\n", terminal: true},
		{name: "non-terminal", wantError: "rerun with --yes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var prompt bytes.Buffer
			confirmed, err := confirmApply(strings.NewReader(test.input), &prompt, test.terminal)

			assert.Equal(t, test.confirmed, confirmed)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
				assert.Contains(t, prompt.String(), "Sync these changes?")
			}
		})
	}
}

func TestReviewAndConfirmPlanWatchPolicy(t *testing.T) {
	tests := []struct {
		name         string
		action       Action
		input        string
		interactive  bool
		yes          bool
		wantContinue bool
		wantError    string
		wantPrompt   bool
	}{
		{name: "updates apply automatically", action: ActionUpdateServer, wantContinue: true},
		{
			name: "server archive requires confirmation", action: ActionArchiveServer, input: "yes\n",
			interactive: true, wantContinue: true, wantPrompt: true,
		},
		{
			name: "local deletion can be declined", action: ActionDeleteLocal, input: "no\n",
			interactive: true, wantPrompt: true,
		},
		{
			name: "non-terminal destructive action is rejected", action: ActionArchiveServer,
			wantError: "rerun with --yes",
		},
		{
			name: "explicit yes applies destructive action", action: ActionArchiveServer,
			yes: true, wantContinue: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			plan := Plan{Resources: []PlannedResource{{ID: testResourceID(), Action: test.action}}}
			continued, err := reviewAndConfirmPlan(Options{
				Watch: true, Yes: test.yes, Input: strings.NewReader(test.input), ErrorOutput: &output,
			}, plan, test.interactive)

			assert.Equal(t, test.wantContinue, continued)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.wantPrompt, strings.Contains(output.String(), "Sync these changes?"))
		})
	}
}
