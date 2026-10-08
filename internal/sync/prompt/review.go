package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

const applyQuestion = "\nSync these changes? [y/N] "

// reviewAndConfirmPlan shows the plan and reports whether to apply it.
//
// A plan that changes only the manifest applies without a question. With
// --yes, every plan applies. In watch mode, a plan without a deletion or an
// archive applies. Every other plan needs the user to agree.
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

	confirmed, err := confirm(options.Context, options.Input, options.ErrorOutput, interactive, applyQuestion)
	if err != nil {
		return false, err
	}
	if !confirmed {
		_ = syncconsole.New(options.ErrorOutput).Line("Sync canceled.")
	}
	return confirmed, nil
}

// confirm asks a yes or no question and stops when ctx ends. A nil ctx never
// ends.
func confirm(ctx context.Context, input io.Reader, output io.Writer, interactive bool, question string) (bool, error) {
	if !interactive {
		return false, errors.New("interactive confirmation requires a terminal; rerun with --yes to apply non-interactively")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// A read from io.Reader cannot stop when ctx ends, so read in a goroutine.
	// The channel has a buffer, so the goroutine can finish after confirm
	// returns.
	type answer struct {
		confirmed bool
		err       error
	}
	answers := make(chan answer, 1)
	go func() {
		confirmed, err := readConfirmation(input, output, question)
		answers <- answer{confirmed: confirmed, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-answers:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return result.confirmed, result.err
	}
}

func readConfirmation(input io.Reader, output io.Writer, question string) (bool, error) {
	if err := syncconsole.New(output).Write(question); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read apply confirmation: %w", err)
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

// cleanupOrphanedAttachments shows the tool and skill files that no local
// variation uses, and deletes them after the user agrees. Without --yes and
// without a terminal, it keeps the files. Before it deletes a file, it makes
// sure again that no variation uses the file.
func cleanupOrphanedAttachments(options Options, store synclocal.Store, interactive bool) error {
	orphaned, err := store.OrphanedAttachments()
	if err != nil || len(orphaned) == 0 {
		return err
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
		_ = console.Printf(
			"    %s %q\n      %s/%s\n", strings.ToUpper(kind[:1])+kind[1:], attachment.Key, syncdomain.RootDir, attachment.Path,
		)
	}

	if !options.Yes && !interactive {
		_ = console.Line("Unreferenced attachment files kept. Rerun with --yes to delete them.")
		return nil
	}
	if !options.Yes {
		confirmed, err := confirm(
			options.Context, options.Input, options.ErrorOutput, interactive,
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

	// A file can become used again while the user reads the question.
	current, err := store.OrphanedAttachments()
	if err != nil {
		return err
	}
	orphaned = slices.DeleteFunc(orphaned, func(attachment synclocal.OrphanedAttachment) bool {
		return !slices.Contains(current, attachment)
	})
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
		_ = console.Printf("- %s/%s\n", syncdomain.RootDir, file)
	}
	return nil
}
