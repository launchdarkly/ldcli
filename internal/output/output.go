package output

import (
	"strings"

	"ldcli/internal/errors"
)

var ErrInvalidOutputKind = errors.NewError("output is invalid")

// Outputter defines the different ways a command's response can be formatted based on
// user input.
type Outputter interface {
	JSON() string
	String() string
}

// OutputterFn is a factory to build the right outputter. By adding an layer of abstraction,
// it lets us push back the error handling from where a caller provides the input to where
// the caller builds the outputter.
type OutputterFn interface {
	New() (Outputter, error)
}

// PlaintextOutputFn represents the various ways to output a resource or resources.
type PlaintextOutputFn[T any] func(t T) string

// resource is the subset of data we need to display a command's plain text response for a single
// resource.
type resource struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// resources is the subset of data we need to display a command's plain text response for a list
// of resources.
type resources struct {
	Items []resource `json:"items"`
}

type configResource map[string]interface{}

// CmdOutput returns a command's response as a string formatted based on the user's requested type.
func CmdOutput(outputKind string, outputter OutputterFn) (string, error) {
	o, err := outputter.New()
	if err != nil {
		return "", err
	}

	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return o.String(), nil
	}

	return "", ErrInvalidOutputKind
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
