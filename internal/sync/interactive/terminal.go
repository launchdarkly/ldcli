// Package interactive contains shared helpers for terminal-based sync flows.
package interactive

import (
	"io"
	"os"

	"golang.org/x/term"
)

// StreamsAreTerminal reports whether both interactive streams are terminals.
func StreamsAreTerminal(input io.Reader, output io.Writer) bool {
	in, inputIsFile := input.(*os.File)
	out, outputIsFile := output.(*os.File)
	return inputIsFile && outputIsFile && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}
