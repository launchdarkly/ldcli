package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
)

func (runner Runner) reviewPlans(
	options Options,
	plans []syncapi.ProjectPlan,
) (bool, error) {
	confirmationOutput := options.ErrorOutput
	if err := writePlanReview(
		confirmationOutput,
		"plaintext",
		plans,
		terminalWidth(confirmationOutput),
	); err != nil {
		return false, err
	}
	if err := validatePlansForSync(plans); err != nil {
		return false, err
	}
	if !plansRequireApply(plans) {
		if options.OutputKind == "plaintext" || options.OutputKind == "" {
			return false, nil
		}
		return false, writePlanOutput(options.Output, options.OutputKind, plans)
	}
	if options.Yes || !plansNeedConfirmation(plans) {
		return true, nil
	}

	confirmed, err := confirmApply(
		options.Input,
		confirmationOutput,
		runner.isTerminal,
	)
	if err != nil {
		return false, err
	}
	if !confirmed {
		_, _ = fmt.Fprintln(confirmationOutput, "Sync canceled.")
	}
	return confirmed, nil
}

type terminalCheck func(io.Reader, io.Writer) bool

func confirmApply(
	input io.Reader,
	prompt io.Writer,
	isTerminal terminalCheck,
) (bool, error) {
	if !isTerminal(input, prompt) {
		return false, fmt.Errorf(
			"interactive apply confirmation requires a terminal; rerun with --yes to apply non-interactively",
		)
	}
	if _, err := fmt.Fprint(prompt, "\nSync these changes? [y/N] "); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read apply confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func terminalStreams(input io.Reader, output io.Writer) bool {
	in, inputIsFile := input.(*os.File)
	out, outputIsFile := output.(*os.File)
	return inputIsFile &&
		outputIsFile &&
		term.IsTerminal(int(in.Fd())) &&
		term.IsTerminal(int(out.Fd()))
}

func writeCompletedSyncOutput(
	out io.Writer,
	outputKind string,
	pulls []serverPull,
	plans []syncapi.ProjectPlan,
	applies []syncapi.ProjectApply,
) error {
	if outputKind == "json" {
		return writeSyncOutput(out, outputKind, pulls, plans, applies)
	}
	return writeSyncOutput(out, outputKind, pulls, nil, applies)
}
