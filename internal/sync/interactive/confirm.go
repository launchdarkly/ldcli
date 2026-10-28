package interactive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Confirm asks a yes or no question and stops when ctx ends. A nil ctx never
// ends. Without a terminal, it returns an error that suggests --yes.
func Confirm(ctx context.Context, input io.Reader, output io.Writer, interactive bool, question string) (bool, error) {
	if !interactive {
		return false, errors.New("interactive confirmation requires a terminal; rerun with --yes to apply non-interactively")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// A read from io.Reader cannot stop when ctx ends, so read in a goroutine.
	// The channel has a buffer, so the goroutine can finish after confirm
	// returns.
	type answer struct {
		confirmed bool
		err       error
	}
	answers := make(chan answer, 1)
	go func() {
		confirmed, err := readConfirmation(input, output, question)
		answers <- answer{confirmed: confirmed, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-answers:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return result.confirmed, result.err
	}
}

func readConfirmation(input io.Reader, output io.Writer, question string) (bool, error) {
	if _, err := io.WriteString(output, question); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}
