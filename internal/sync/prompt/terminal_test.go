package prompt

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type notifyingWriter struct {
	io.Writer
	once    sync.Once
	written chan struct{}
}

func (writer *notifyingWriter) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte("Sync these changes?")) {
		writer.once.Do(func() { close(writer.written) })
	}
	return writer.Writer.Write(data)
}

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

func TestReviewAndConfirmPlanStopsWhenWatchContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input, inputWriter := io.Pipe()
	defer input.Close()
	defer inputWriter.Close()

	promptStarted := make(chan struct{})
	output := &notifyingWriter{Writer: io.Discard, written: promptStarted}
	results := make(chan struct {
		confirmed bool
		err       error
	}, 1)
	go func() {
		confirmed, err := reviewAndConfirmPlan(Options{
			Watch: true, Context: ctx, Input: input, ErrorOutput: output,
		}, Plan{Resources: []PlannedResource{{
			ID: testResourceID(), Action: ActionArchiveServer,
		}}}, true)
		results <- struct {
			confirmed bool
			err       error
		}{confirmed: confirmed, err: err}
	}()

	select {
	case <-promptStarted:
	case <-time.After(time.Second):
		t.Fatal("confirmation prompt was not written")
	}
	cancel()

	select {
	case result := <-results:
		require.False(t, result.confirmed)
		require.ErrorIs(t, result.err, context.Canceled)
	case <-time.After(time.Second):
		_, err := io.WriteString(inputWriter, "yes\n")
		require.NoError(t, err)
		result := <-results
		t.Fatalf("confirmation remained blocked after cancellation and later returned confirmed=%v, err=%v", result.confirmed, result.err)
	}
}

func TestReviewAndConfirmPlanRejectsConfirmationWhenWatchContextIsAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	confirmed, err := reviewAndConfirmPlan(Options{
		Watch: true, Context: ctx, Input: strings.NewReader("yes\n"), ErrorOutput: io.Discard,
	}, Plan{Resources: []PlannedResource{{
		ID: testResourceID(), Action: ActionArchiveServer,
	}}}, true)

	require.False(t, confirmed)
	require.ErrorIs(t, err, context.Canceled)
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
