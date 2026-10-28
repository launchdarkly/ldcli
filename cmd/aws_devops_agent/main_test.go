package awsdevopsagent

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	newChooser = lineChooser
	os.Exit(m.Run())
}

// lineChooser stands in for the picker, which needs a terminal. It prints the
// title and the numbered labels, and reads a number from stdin. An empty line
// picks the start label. No line, or a line that is not a number of a label,
// cancels.
func lineChooser(cmd *cobra.Command) chooser {
	return func(title, _ string, labels []string, start int) (int, error) {
		out := cmd.ErrOrStderr()
		_, _ = fmt.Fprintln(out, title)
		for i, label := range labels {
			_, _ = fmt.Fprintf(out, "  %d) %s\n", i+1, label)
		}
		_, _ = fmt.Fprintf(out, "start %d\n", start+1)

		line, err := nextLine(sharedStdin(cmd))
		if err != nil && line == "" {
			return 0, errPickCanceled
		}
		answer := strings.TrimSpace(line)
		if answer == "" {
			return start, nil
		}
		choice, err := strconv.Atoi(answer)
		if err != nil || choice < 1 || choice > len(labels) {
			return 0, errPickCanceled
		}

		return choice - 1, nil
	}
}
