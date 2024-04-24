package output

import (
	"encoding/json"
	"fmt"
)

// SingularPlaintextOutputFn converts the resource to plain text based on its name and key.
var SingularPlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("%s (%s)", r["name"], r["key"])
}

type singularOutputterFn struct {
	input []byte
}

// New unmarshals a single resource and wires up a particular plain text output function.
func (o singularOutputterFn) New() (Outputter, error) {
	var r resource
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return SingularOutputter{}, err
	}

	return SingularOutputter{
		outputFn:     SingularPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}

func NewSingularOutput(input []byte) singularOutputterFn {
	return singularOutputterFn{
		input: input,
	}
}

type SingularOutputter struct {
	outputFn     PlaintextOutputFn[resource]
	resource     resource
	resourceJSON []byte
}

func (o SingularOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o SingularOutputter) String() string {
	return formatColl([]resource{o.resource}, o.outputFn)
}

type SingularOutputter2 struct {
	outputFn     PlaintextOutputFn2
	resource     resource
	resourceJSON []byte
}

func (o SingularOutputter2) JSON() string {
	return string(o.resourceJSON)
}

func (o SingularOutputter2) String() string {
	return formatColl([]resource{o.resource}, o.outputFn)
}
