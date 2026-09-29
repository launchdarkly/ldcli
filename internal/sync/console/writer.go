// Package console provides the shared writer used for sync command output.
package console

import (
	"fmt"
	"io"
)

// Writer writes user-facing sync output to a command stream.
type Writer struct {
	output io.Writer
}

// New creates a console writer for output.
func New(output io.Writer) Writer {
	return Writer{output: output}
}

// Write writes text without adding a newline.
func (writer Writer) Write(text string) error {
	_, err := io.WriteString(writer.output, text)
	return err
}

// WriteBytes writes bytes without adding a newline.
func (writer Writer) WriteBytes(content []byte) error {
	_, err := writer.output.Write(content)
	return err
}

// Line writes text followed by a newline.
func (writer Writer) Line(text string) error {
	_, err := fmt.Fprintln(writer.output, text)
	return err
}

// Printf writes formatted text.
func (writer Writer) Printf(format string, values ...any) error {
	_, err := fmt.Fprintf(writer.output, format, values...)
	return err
}
