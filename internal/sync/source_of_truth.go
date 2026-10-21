package sync

import "fmt"

// SourceOfTruth is the side that can change the prompt of a synced variation.
type SourceOfTruth string

const (
	// SourceOfTruthUIAndCode lets the LaunchDarkly UI and the local file both change a variation.
	SourceOfTruthUIAndCode SourceOfTruth = "ui-and-code"
	// SourceOfTruthCodeOnly lets only the local file change the prompt. The other settings sync both ways.
	SourceOfTruthCodeOnly SourceOfTruth = "code-only"
)

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
			"LaunchDarkly rejects a prompt change that does not come from sync. The other settings sync both ways."
	}
	return "The LaunchDarkly UI and the local files can now both change these variations. " +
		"If both sides change a variation, sync asks which side to keep."
}
