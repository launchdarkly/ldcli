package prompt

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
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

type callbackReader struct {
	io.Reader
	once     sync.Once
	callback func()
}

func (reader *callbackReader) Read(data []byte) (int, error) {
	reader.once.Do(reader.callback)
	return reader.Reader.Read(data)
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
			confirmed, err := syncinteractive.Confirm(context.Background(), strings.NewReader(test.input), &prompt, test.terminal, applyQuestion)

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

func TestCleanupOrphanedAttachmentsRequiresConfirmationUnlessYes(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		yes         bool
		interactive bool
		wantExist   bool
		message     string
	}{
		{name: "declined", input: "n\n", interactive: true, wantExist: true, message: "files kept"},
		{name: "yes flag", yes: true, message: "Deleted unreferenced attachment files"},
		{name: "non-terminal", wantExist: true, message: "Rerun with --yes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, store, _ := newOrphanedToolStore(t)

			var output bytes.Buffer
			err := cleanupOrphanedAttachments(Options{
				Yes: test.yes, Input: strings.NewReader(test.input), ErrorOutput: &output,
			}, store, test.interactive)

			require.NoError(t, err)
			_, statErr := os.Stat(filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"))
			assert.Equal(t, test.wantExist, statErr == nil)
			assert.Contains(t, output.String(), `Tool "search"`)
			assert.Contains(t, output.String(), test.message)
			if test.yes {
				assert.NotContains(t, output.String(), "Delete these unreferenced local files?")
			}
		})
	}
}

func TestCleanupOrphanedAttachmentsRechecksReferencesAfterConfirmation(t *testing.T) {
	root, store, attachedVariation := newOrphanedToolStore(t)
	input := &callbackReader{
		Reader: strings.NewReader("yes\n"),
		callback: func() {
			_, err := store.ReplaceVariations([]synclocal.VariationReplacement{{
				ProjectKey: "project",
				ConfigKey:  "config",
				Variation:  attachedVariation,
			}})
			require.NoError(t, err)
		},
	}
	var output bytes.Buffer

	err := cleanupOrphanedAttachments(Options{
		Input: input, ErrorOutput: &output,
	}, store, true)

	require.NoError(t, err)
	_, statErr := os.Stat(filepath.Join(root, ".launchdarkly", "project", "tools", "search.json"))
	require.NoError(t, statErr)
	assert.Contains(t, output.String(), "No selected attachment files remain unreferenced.")
}

func newOrphanedToolStore(t *testing.T) (string, synclocal.Store, syncdomain.Variation) {
	t.Helper()
	root := t.TempDir()
	store := synclocal.NewStore(root)
	tool := syncdomain.Tool{Key: "search", Schema: map[string]any{"type": "object"}}
	attached := syncdomain.Variation{
		Mode: syncdomain.VariationModeAgent, Key: "default", Name: "Default",
		Tools: []syncdomain.AttachmentRef{{Key: tool.Key}},
		Attachments: []syncdomain.Attachment{{
			Kind: syncdomain.AttachmentTool, Tool: &tool,
		}},
	}
	_, err := store.Add([]synclocal.VariationFile{{
		ProjectKey: "project", ConfigKey: "config", Variation: attached,
	}})
	require.NoError(t, err)

	orphaned := attached
	orphaned.Tools = nil
	orphaned.Attachments = nil
	_, err = store.ReplaceVariations([]synclocal.VariationReplacement{{
		ProjectKey: "project", ConfigKey: "config", Variation: orphaned,
	}})
	require.NoError(t, err)
	return root, store, attached
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
			Action: SyncAction{Watch: true}, Context: ctx, Input: input, ErrorOutput: output,
		}, Plan{Resources: []PlannedResource{{
			ID: testResourceID(), Action: ActionDeleteLocal,
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
		Action: SyncAction{Watch: true}, Context: ctx, Input: strings.NewReader("yes\n"), ErrorOutput: io.Discard,
	}, Plan{Resources: []PlannedResource{{
		ID: testResourceID(), Action: ActionDeleteLocal,
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
			name: "local deletion requires confirmation", action: ActionDeleteLocal, input: "yes\n",
			interactive: true, wantContinue: true, wantPrompt: true,
		},
		{
			name: "local deletion can be declined", action: ActionDeleteLocal, input: "no\n",
			interactive: true, wantPrompt: true,
		},
		{
			name: "non-terminal destructive action is rejected", action: ActionDeleteLocal,
			wantError: "rerun with --yes",
		},
		{
			name: "explicit yes applies destructive action", action: ActionDeleteLocal,
			yes: true, wantContinue: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			plan := Plan{Resources: []PlannedResource{{ID: testResourceID(), Action: test.action}}}
			continued, err := reviewAndConfirmPlan(Options{
				Action: SyncAction{Watch: true}, Yes: test.yes, Input: strings.NewReader(test.input), ErrorOutput: &output,
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
