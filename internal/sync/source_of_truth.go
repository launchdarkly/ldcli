package sync

import (
	"errors"
	"fmt"
)

// SourceOfTruth is the side that can change the prompt of a synced variation.
type SourceOfTruth string

const (
	SourceOfTruthUIAndCode SourceOfTruth = "ui-and-code"
	// SourceOfTruthCodeOnly lets only the local file change the prompt. The other settings sync both ways.
	SourceOfTruthCodeOnly SourceOfTruth = "code-only"
)

// ErrNoSourceOfTruth means that add needs a source of truth and cannot ask for it.
var ErrNoSourceOfTruth = errors.New("add needs a source of truth. Use --source-of-truth ui-and-code or code-only. " +
	"With ui-and-code, the LaunchDarkly UI and the local files can both change the variations. " +
	"With code-only, only the local files can change the prompts")

// ParseSourceOfTruth returns the source of truth that value names.
func ParseSourceOfTruth(value string) (SourceOfTruth, error) {
	switch source := SourceOfTruth(value); source {
	case SourceOfTruthUIAndCode, SourceOfTruthCodeOnly:
		return source, nil
	default:
		return "", fmt.Errorf("the source of truth must be %s or %s, not %q", SourceOfTruthUIAndCode, SourceOfTruthCodeOnly, value)
	}
}

// Summary tells the user what the source of truth of the variations now is.
func (source SourceOfTruth) Summary() string {
	if source == SourceOfTruthCodeOnly {
		return "The local files are now the source of truth for the prompts of these variations. " +
			"LaunchDarkly rejects a prompt change that does not come from sync. The other settings sync both ways. " +
			"To let the LaunchDarkly UI change a prompt again, change the source of truth in the variation menu in LaunchDarkly."
	}
	return "The LaunchDarkly UI and the local files can now both change these variations. " +
		"If both sides change a variation, sync asks which side to keep."
}
