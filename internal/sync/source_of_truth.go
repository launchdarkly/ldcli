package sync

// SourceOfTruth is the side that can change the prompt of a synced variation.
type SourceOfTruth string

const (
	SourceOfTruthUIAndCode SourceOfTruth = "ui-and-code"
	// SourceOfTruthCodeOnly lets only the local file change the prompt. The other settings sync both ways.
	SourceOfTruthCodeOnly SourceOfTruth = "code-only"
)
