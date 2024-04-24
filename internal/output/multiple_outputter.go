package output

import (
	"encoding/json"
	"fmt"
)

// MultiplePlaintextOutputFn converts the resource to plain text based on its name and key in a list.
var MultiplePlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("* %s (%s)", r["name"], r["key"])
}

type multipleOutputterFn struct {
	input []byte
}

// New unmarshals multiple resources and wires up a particular plain text output function.
func (o multipleOutputterFn) New() (Outputter, error) {
	var r resources
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return MultipleOutputter{}, err
	}

	return MultipleOutputter{
		outputFn:     MultiplePlaintextOutputFn,
		resources:    r,
		resourceJSON: o.input,
	}, nil
}

func NewMultipleOutput(input []byte) multipleOutputterFn {
	return multipleOutputterFn{
		input: input,
	}
}

type MultipleOutputter struct {
	outputFn     PlaintextOutputFn[resource]
	resources    resources
	resourceJSON []byte
}

func (o MultipleOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o MultipleOutputter) String() string {
	return formatColl(o.resources.Items, o.outputFn)
}

type MultipleOutputter2 struct {
	outputFn     PlaintextOutputFn2
	resources    resources
	resourceJSON []byte
}

func (o MultipleOutputter2) JSON() string {
	return string(o.resourceJSON)
}

func (o MultipleOutputter2) String() string {
	return formatColl(o.resources.Items, o.outputFn)
}
