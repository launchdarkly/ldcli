//go:build !darwin && !linux

package render

// TerminalWidth is unavailable on other platforms; tables render unconstrained.
func TerminalWidth() int { return 0 }
