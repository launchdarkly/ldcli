package output

import (
	"encoding/json"

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
type PlaintextOutputFn func(resource) string

// resource is the subset of data we need to display a command's plain text response for a single
// resource.
// We're trading off type safety for easy of use instead of defining a type for each expected resource.
type resource map[string]interface{}

// resources is the subset of data we need to display a command's plain text response for a list
// of resources.
type resources struct {
	Items []resource `json:"items"`
}

// resourcesBare is for responses that return a list of resources at the top level of the response,
// not as a value of an "items" property.
type resourcesBare []resource

// CmdOutputSingular builds a command response based on the flag the user provided and the shape of
// the input. The expected shape is a single JSON object.
func CmdOutputSingular(outputKind string, input []byte, fn PlaintextOutputFn) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	return outputFromKind(outputKind, "", SingularOutputter{
		outputFn:     fn,
		resource:     r,
		resourceJSON: input,
	})
}

// CmdOutputCreate builds a command response based on the flag the user provided and the shape of
// the input with a successfully created message. The expected shape is a single JSON object.
func CmdOutputCreate(outputKind string, input []byte, fn PlaintextOutputFn) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	return outputFromKind(outputKind, "Successfully created ", SingularOutputter{
		outputFn:     fn,
		resource:     r,
		resourceJSON: input,
	})
}

// CmdOutputUpdate builds a command response based on the flag the user provided and the shape of
// the input with a successfully created message. The expected shape is a single JSON object.
func CmdOutputUpdate(outputKind string, input []byte, fn PlaintextOutputFn) (string, error) {
	var r resource
	err := json.Unmarshal(input, &r)
	if err != nil {
		return "", err
	}

	return outputFromKind(outputKind, "Successfully updated ", SingularOutputter{
		outputFn:     fn,
		resource:     r,
		resourceJSON: input,
	})
}

// CmdOutputMultiple builds a command response based on the flag the user provided and the shape of
// the input. The expected shape is a list of JSON objects.
func CmdOutputMultiple(outputKind string, input []byte, fn PlaintextOutputFn) (string, error) {
	var r resources
	err := json.Unmarshal(input, &r)
	if err != nil {
		// sometimes a response doesn't include each item in an "items" property
		var rr resourcesBare
		err := json.Unmarshal(input, &rr)
		if err != nil {
			return "", err
		}
		r.Items = rr
	}

	return outputFromKind(outputKind, "", MultipleOutputter{
		outputFn:     fn,
		resources:    r,
		resourceJSON: input,
	})
}

func outputFromKind(outputKind string, additional string, o Outputter) (string, error) {
	switch outputKind {
	case "json":
		return o.JSON(), nil
	case "plaintext":
		return additional + o.String(), nil
	}

	return "", ErrInvalidOutputKind
}
