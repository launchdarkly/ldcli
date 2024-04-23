package output

import (
	"encoding/json"
	"fmt"
)

// errorPlaintextOutputFn converts the resource to plain text specifically for data from the
// error file.
var errorPlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("%s (code: %s)", r["message"], r["code"])
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
		outputFn:     errorPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}

func NewErrorOutput(input []byte) errorOutputterFn {
	return errorOutputterFn{
		input: input,
	}
}
