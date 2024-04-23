package output

import (
	"encoding/json"
	"fmt"
)

// multiplePlaintextOutputFn converts the resource to plain text based on its name and key in a list.
var multiplePlaintextOutputFn = func(r resource) string {
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
		outputFn:     multiplePlaintextOutputFn,
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
