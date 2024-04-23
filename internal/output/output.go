package output

import (
	"strings"

	"ldcli/internal/errors"
)

var ErrInvalidOutputKind = errors.NewError("output is invalid")

// Outputter defines the different ways a command's response can be formatted. Every command will
// need to implement its own type based on its data's representation.
type Outputter interface {
	JSON() string
	String() string
}

// CmdOutput returns a command's response as a string formatted based on the user's requested type.
func CmdOutput(outputKind string, outputter Outputter) string {
	switch outputKind {
	case "json":
		return outputter.JSON()
	case "plaintext":
		return outputter.String()
	}

	return ""
}

// FormatColl applies a formatting function to every element in the collection and returns it as a
// string.
func formatColl[T any](coll []T, formatFn func(T) string) string {
	lst := make([]string, 0, len(coll))
	for _, c := range coll {
		lst = append(lst, formatFn(c))
	}

	return strings.Join(lst, "\n")
}
