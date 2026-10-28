package awsdevopsagent

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// sharedStdin gives every prompt one reader.
func sharedStdin(cmd *cobra.Command) io.Reader {
	switch in := cmd.InOrStdin().(type) {
	case *os.File, *bufio.Reader:
		return in
	default:
		shared := bufio.NewReader(in)
		cmd.SetIn(shared)

		return shared
	}
}

// readLine reads one line, which the terminal echoes as it is typed.
func readLine(in io.Reader) string {
	line, _ := nextLine(in)

	return strings.TrimSpace(line)
}

// nextLine reads up to and including the next newline. It reads a file one
// byte at a time, so the lines after it stay for the next prompt.
func nextLine(in io.Reader) (string, error) {
	if reader, ok := in.(*bufio.Reader); ok {
		return reader.ReadString('\n')
	}

	var line []byte
	b := make([]byte, 1)
	for {
		n, err := in.Read(b)
		if n > 0 {
			line = append(line, b[0])
			if b[0] == '\n' {
				return string(line), nil
			}
		}
		if err != nil {
			return string(line), err
		}
	}
}

// readYes reads the answer to a [Y/n] question. An empty line is a yes, so
// Enter accepts. The end of the input is a no.
func readYes(in io.Reader) bool {
	line, err := nextLine(in)
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	}

	return false
}

// canPrompt reports a terminal on stdin and stderr. The prompts go to stderr,
// so a run with stderr sent to a file does not wait for an answer.
func canPrompt() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}
