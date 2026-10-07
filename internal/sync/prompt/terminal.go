package prompt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
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
	autoApply := options.Yes || (options.Watch && !plan.HasDestructiveActions())
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
	if ctx == nil {
		return confirmApply(input, prompt, interactive)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// io.Reader has no context-aware read contract. Isolate the blocking read
	// so cancellation can return immediately; the buffered channel lets the
	// reader finish without waiting for a receiver after the caller exits.
	result := make(chan confirmationResult, 1)
	go func() {
		confirmed, err := confirmApply(input, prompt, interactive)
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
	if !interactive {
		return false, fmt.Errorf("interactive apply confirmation requires a terminal; rerun with --yes to apply non-interactively")
	}
	if err := syncconsole.New(prompt).Write("\nSync these changes? [y/N] "); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read apply confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}
