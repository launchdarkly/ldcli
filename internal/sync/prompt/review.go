package prompt

import (
	"errors"

	syncconsole "github.com/launchdarkly/ldcli/internal/sync/console"
	syncinteractive "github.com/launchdarkly/ldcli/internal/sync/interactive"
)

const applyQuestion = "\nSync these changes? [y/N] "

// reviewAndConfirmPlan reports whether to apply the plan. It shows the plan
// only when the plan has nothing to apply, when a row blocks the sync, or
// before it asks the user.
//
// A plan that changes only the manifest applies without a question. With
// --yes, every plan applies. In watch mode, a plan without a deletion or an
// archive applies. Every other plan needs the user to agree.
func reviewAndConfirmPlan(options Options, plan Plan, interactive bool) (bool, error) {
	writePlanNotes(options.ErrorOutput, plan)
	review := func() error {
		return writePlanReview(options.ErrorOutput, outputPlaintext, plan, terminalWidth(options.ErrorOutput), options.Diff)
	}
	if err := plan.BlockingError(); err != nil {
		return false, errors.Join(review(), err)
	}
	if !plan.HasChanges() {
		if options.OutputKind != "" && options.OutputKind != outputPlaintext {
			return false, writePlanOutput(options.Output, options.OutputKind, plan, options.Diff)
		}
		return false, review()
	}
	autoApply := options.Yes || (options.watching() && !plan.HasDestructiveActions())
	if autoApply || !plan.RequiresConfirmation() {
		return true, nil
	}

	if err := review(); err != nil {
		return false, err
	}
	confirmed, err := syncinteractive.Confirm(options.Context, options.Input, options.ErrorOutput, interactive, applyQuestion)
	if err != nil {
		return false, err
	}
	if !confirmed {
		_ = syncconsole.New(options.ErrorOutput).Line("Sync canceled.")
	}
	return confirmed, nil
}
