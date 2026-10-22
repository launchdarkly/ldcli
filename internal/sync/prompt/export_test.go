package prompt

import (
	"io"

	"github.com/launchdarkly/ldcli/internal/resources"
)

// NewTerminalRunner returns a runner whose streams count as a terminal, so
// that a test can answer its questions.
func NewTerminalRunner(client resources.Client) Runner {
	runner := NewRunner(client)
	runner.isTerminal = func(io.Reader, io.Writer) bool { return true }
	return runner
}
