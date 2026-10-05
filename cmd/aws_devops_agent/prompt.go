package awsdevopsagent

import (
	"bufio"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// readLine reads one line, which the terminal echoes as it is typed.
func readLine(in io.Reader) string {
	line, _ := bufio.NewReader(in).ReadString('\n')

	return strings.TrimSpace(line)
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
