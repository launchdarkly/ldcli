package prompt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

// reviewAndConfirmPlan renders a plan and decides whether execution should continue.
func reviewAndConfirmPlan(options Options, plan Plan, interactive bool) (bool, error) {
	if err := writePlanReview(options.ErrorOutput, "plaintext", plan, terminalWidth(options.ErrorOutput)); err != nil {
		return false, err
	}
	if err := plan.BlockingError(); err != nil {
		return false, err
	}
	if !plan.HasChanges() {
		if options.OutputKind != "" && options.OutputKind != "plaintext" {
			return false, writePlanOutput(options.Output, options.OutputKind, plan)
		}
		return false, nil
	}
	autoApply := options.Yes || (options.watching() && !plan.HasDestructiveActions())
	if autoApply || !plan.RequiresConfirmation() {
		return true, nil
	}

	confirmed, err := confirmApplyWithContext(options.Context, options.Input, options.ErrorOutput, interactive)
	if err != nil {
		return false, err
	}
	if !confirmed {
		_ = syncconsole.New(options.ErrorOutput).Line("Sync canceled.")
	}
	return confirmed, nil
}

type terminalCheck func(io.Reader, io.Writer) bool

type confirmationResult struct {
	confirmed bool
	err       error
}

func confirmApplyWithContext(ctx context.Context, input io.Reader, prompt io.Writer, interactive bool) (bool, error) {
	return confirmQuestionWithContext(ctx, input, prompt, interactive, "\nSync these changes? [y/N] ")
}

func confirmQuestionWithContext(ctx context.Context, input io.Reader, prompt io.Writer, interactive bool, question string) (bool, error) {
	if ctx == nil {
		return confirmQuestion(input, prompt, interactive, question)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// io.Reader has no context-aware read contract. Isolate the blocking read
	// so cancellation can return immediately; the buffered channel lets the
	// reader finish without waiting for a receiver after the caller exits.
	result := make(chan confirmationResult, 1)
	go func() {
		confirmed, err := confirmQuestion(input, prompt, interactive, question)
		result <- confirmationResult{confirmed: confirmed, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case confirmation := <-result:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return confirmation.confirmed, confirmation.err
	}
}

// confirmApply asks an interactive user to approve planned changes.
func confirmApply(input io.Reader, prompt io.Writer, interactive bool) (bool, error) {
	return confirmQuestion(input, prompt, interactive, "\nSync these changes? [y/N] ")
}

func confirmQuestion(input io.Reader, prompt io.Writer, interactive bool, question string) (bool, error) {
	if !interactive {
		return false, fmt.Errorf("interactive confirmation requires a terminal; rerun with --yes to apply non-interactively")
	}
	if err := syncconsole.New(prompt).Write(question); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read apply confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// cleanupOrphanedAttachments removes local dependency files after confirmation
// and a final reference check.
func cleanupOrphanedAttachments(options Options, store synclocal.Store, interactive bool) error {
	orphaned, err := store.OrphanedAttachments()
	if err != nil {
		return err
	}
	if len(orphaned) == 0 {
		return nil
	}

	console := syncconsole.New(options.ErrorOutput)
	_ = console.Line("\nUnreferenced local attachment files:")
	currentProject := ""
	for _, attachment := range orphaned {
		if attachment.ProjectKey != currentProject {
			currentProject = attachment.ProjectKey
			_ = console.Printf("  Project: %s\n", currentProject)
		}
		kind := string(attachment.Kind)
		kind = strings.ToUpper(kind[:1]) + kind[1:]
		_ = console.Printf(
			"    %s %q\n      %s/%s\n",
			kind,
			attachment.Key,
			".launchdarkly",
			attachment.Path,
		)
	}

	if !options.Yes && !interactive {
		_ = console.Line("Unreferenced attachment files kept. Rerun with --yes to delete them.")
		return nil
	}
	if !options.Yes {
		confirmed, err := confirmQuestionWithContext(
			options.Context,
			options.Input,
			options.ErrorOutput,
			interactive,
			"\nDelete these unreferenced local files? [y/N] ",
		)
		if err != nil {
			return err
		}
		if !confirmed {
			_ = console.Line("Unreferenced attachment files kept.")
			return nil
		}
	}

	currentOrphans, err := store.OrphanedAttachments()
	if err != nil {
		return err
	}
	orphaned = stillOrphanedAttachments(orphaned, currentOrphans)
	if len(orphaned) == 0 {
		_ = console.Line("No selected attachment files remain unreferenced.")
		return nil
	}

	deleted, err := store.DeleteAttachments(orphaned)
	if err != nil {
		return err
	}
	_ = console.Line("Deleted unreferenced attachment files:")
	for _, file := range deleted {
		_ = console.Printf("- %s/%s\n", ".launchdarkly", file)
	}
	return nil
}

func stillOrphanedAttachments(reviewed, current []synclocal.OrphanedAttachment) []synclocal.OrphanedAttachment {
	currentSet := make(map[synclocal.OrphanedAttachment]struct{}, len(current))
	for _, attachment := range current {
		currentSet[attachment] = struct{}{}
	}
	stillOrphaned := make([]synclocal.OrphanedAttachment, 0, len(reviewed))
	for _, attachment := range reviewed {
		if _, ok := currentSet[attachment]; ok {
			stillOrphaned = append(stillOrphaned, attachment)
		}
	}
	return stillOrphaned
}
