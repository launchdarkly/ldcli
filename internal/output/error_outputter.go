package output

import (
	"encoding/json"
	"fmt"
)

// ErrorPlaintextOutputFn converts the resource to plain text specifically for data from the
// error file.
var ErrorPlaintextOutputFn = func(r resource) string {
	switch {
	case r["code"] == nil && r["message"] == "":
		return "unknown error occurred"
	case r["code"] == nil:
		return r["message"].(string)
	case r["message"] == "":
		return fmt.Sprintf("an error occurred (code: %s)", r["code"])
	default:
		return fmt.Sprintf("%s (code: %s)", r["message"], r["code"])
	}
}

type errorOutputterFn struct {
	input []byte
}

// New unmarshals a single error resource and wires up a particular plain text output function.
func (o errorOutputterFn) New() (Outputter, error) {
	var r resource
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return SingularOutputter{}, err
	}

	return SingularOutputter{
		outputFn:     ErrorPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}

func NewErrorOutput(input []byte) errorOutputterFn {
	return errorOutputterFn{
		input: input,
	}
}

var MultipleEmailPlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("* %s (%s)", r["email"], r["_id"])
}
