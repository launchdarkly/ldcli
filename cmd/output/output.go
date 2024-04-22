package output

import (
	"ldcli/internal/errors"
	"strings"
)

type OutputKind string

const (
	OutputKindJSON      = OutputKind("json")
	OutputKindNone      = OutputKind("")
	OutputKindPlaintext = OutputKind("plaintext")
)

var ErrInvalidOutputKind = errors.NewError("invalid output")

func NewOutputKind(k string) (OutputKind, error) {
	switch k {
	case "json":
		return OutputKindJSON, nil
	case "plaintext":
		return OutputKindPlaintext, nil
	case "":
		return OutputKindPlaintext, nil
	default:
		return OutputKindNone, ErrInvalidOutputKind
	}
}

// Outputter defines the different ways a command's response can be formatted. Every command will
// need to implement its own type based on its data's representation.
type Outputter interface {
	JSON() (string, error)
	String() string
}

// CmdOutput returns a command's response as a string formatted based on the user's requested type.
func CmdOutput(rawOutputKind string, outputter Outputter) (string, error) {
	outputKind, err := NewOutputKind(rawOutputKind)
	if err != nil {
		return "", err
	}

	switch outputKind {
	case OutputKindJSON:
		return outputter.JSON()
	case OutputKindPlaintext:
		return outputter.String(), nil
	}

	return "", ErrInvalidOutputKind
}

// FormatColl applies a formatting function to every element in the collection and returns it as a
// string.
func FormatColl[T any](coll []T, formatFn func(T) string) string {
	lst := make([]string, 0, len(coll))
	for _, c := range coll {
		lst = append(lst, formatFn(c))
	}

	return strings.Join(lst, "\n")
}
