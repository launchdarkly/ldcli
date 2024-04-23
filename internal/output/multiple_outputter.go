package output

import (
	"encoding/json"
	"fmt"
)

var multiplePlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("* %s (%s)", r.Name, r.Key)
}

// TODO: rename this to be "cleaner"? -- NewMultipleOutput()
func NewMultipleOutputterFn(input []byte) multipleOutputterFn {
	return multipleOutputterFn{
		input: input,
	}
}

type multipleOutputterFn struct {
	input []byte
}

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

type MultipleOutputter struct {
	outputFn     PlaintextOutputFn
	resources    resources
	resourceJSON []byte
}

func (o MultipleOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o MultipleOutputter) String() string {
	return formatColl(o.resources.Items, o.outputFn)
}
