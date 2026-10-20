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

// readSecret reads one line without echoing it, so a pasted credential stays
// off the screen.
func readSecret(in io.Reader) string {
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return readLine(in)
	}

	typed, err := term.ReadPassword(int(file.Fd()))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(typed))
}

func canPrompt() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
