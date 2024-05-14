package output

import (
	"encoding/json"

	"ldcli/internal/errors"
)

var ErrInvalidOutputKind = errors.NewError("output is invalid, use 'json' or 'plaintext'")

type OutputKind string

func (o OutputKind) String() string {
	return string(o)
}

var (
	OutputKindJSON      = OutputKind("json")
	OutputKindNull      = OutputKind("")
	OutputKindPlaintext = OutputKind("plaintext")
)

func NewOutputKind(s string) (OutputKind, error) {
	validKinds := map[string]struct{}{
		OutputKindJSON.String():      {},
		OutputKindPlaintext.String(): {},
	}
	if _, isValid := validKinds[s]; !isValid {
		return OutputKindNull, ErrInvalidOutputKind
	}

	return OutputKind(s), nil
}

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
type PlaintextOutputFn func(resource) string

// resource is the subset of data we need to display a command's plain text response for a single
// resource. We're trading off type safety for easy of use instead of defining a type for each
// expected resource.
type resource map[string]interface{}

type link map[string]string

type links map[string]link

// resources is the subset of data we need to display a command's plain text response for a list
// of resources.
type resources struct {
	Items      []resource `json:"items"`
	Links      links      `json:"_links"`
	TotalCount int        `json:"totalCount"`
}

// resourcesList is a response that has a list of scalar values instead of JSON objects.
type resourcesList struct {
	Items      []interface{} `json:"items"`
	Links      links         `json:"_links"`
	TotalCount int           `json:"totalCount"`
}

// CmdOutputSingular builds a command response based on the flag the user provided and the shape of
// the input. The expected shape is a single JSON object.
func CmdOutputSingular(outputKind string, input []byte, fn PlaintextOutputFn) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	return outputFromKind(outputKind, SingularOutputter{
		outputFn:     fn,
		resource:     r,
		resourceJSON: input,
	})
}

func outputFromKind(outputKind string, o Outputter) (string, error) {
	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return o.String(), nil
	}

	return "", ErrInvalidOutputKind
}
