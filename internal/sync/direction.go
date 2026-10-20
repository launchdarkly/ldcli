package sync

import "fmt"

// Direction is the side of a sync that is the source of truth for one
// variation. A rule in the sync settings of a project sets it.
type Direction string

const (
	// DirectionCodeCanonical makes the local file win. Sync writes each local
	// change to LaunchDarkly and reverts each change made in LaunchDarkly.
	DirectionCodeCanonical Direction = "code_canonical"
	// DirectionServerCanonical makes LaunchDarkly win. Sync writes each
	// LaunchDarkly change to the local file and discards each local change.
	DirectionServerCanonical Direction = "server_canonical"
	// DirectionBoth lets each side change the variation. Sync reports a
	// conflict when both sides change it.
	DirectionBoth Direction = "both"
)

// Normalized returns DirectionBoth for an empty direction.
func (direction Direction) Normalized() Direction {
	if direction == "" {
		return DirectionBoth
	}
	return direction
}

// ParseDirection returns the direction that value names. An empty value is
// DirectionBoth.
func ParseDirection(value string) (Direction, error) {
	switch direction := Direction(value).Normalized(); direction {
	case DirectionCodeCanonical, DirectionServerCanonical, DirectionBoth:
		return direction, nil
	default:
		return "", fmt.Errorf("unsupported sync direction %q", value)
	}
}

// The two values that "ldcli sync prompts source-of-truth" accepts.
const (
	// SourceOfTruthUIAndCode lets the LaunchDarkly UI and the local file both
	// change a variation. It is DirectionBoth.
	SourceOfTruthUIAndCode = "ui-and-code"
	// SourceOfTruthCodeOnly makes the local file the only source of truth. It
	// is DirectionCodeCanonical.
	SourceOfTruthCodeOnly = "code-only"
)

// SourceOfTruthValues lists the source of truth values for help text and
// errors.
const SourceOfTruthValues = SourceOfTruthUIAndCode + " or " + SourceOfTruthCodeOnly

// ParseSourceOfTruth returns the direction that a source of truth value
// names.
func ParseSourceOfTruth(value string) (Direction, error) {
	switch value {
	case SourceOfTruthUIAndCode:
		return DirectionBoth, nil
	case SourceOfTruthCodeOnly:
		return DirectionCodeCanonical, nil
	default:
		return "", fmt.Errorf("the source of truth must be %s, not %q", SourceOfTruthValues, value)
	}
}
