// Package interactive has the terminal forms and selectors that the sync
// commands use when the user does not give every argument.
package interactive

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// StreamsAreTerminal reports whether both streams are terminals.
func StreamsAreTerminal(input io.Reader, output io.Writer) bool {
	in, inputIsFile := input.(*os.File)
	out, outputIsFile := output.(*os.File)
	return inputIsFile && outputIsFile && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

// RequireTerminal returns an error if the command cannot ask the user for
// input. The error names the arguments that replace the interactive input
// and the activity that needs a terminal, for example "prompt selection".
func RequireTerminal(input io.Reader, output io.Writer, noInput bool, arguments, activity string) error {
	if noInput {
		return fmt.Errorf("%s are required with --no-input", arguments)
	}
	if !StreamsAreTerminal(input, output) {
		return fmt.Errorf("interactive %s requires a terminal; use %s with --no-input", activity, arguments)
	}
	return nil
}
