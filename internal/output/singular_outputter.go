package output

import (
	"encoding/json"
	"fmt"
)

var singularPlaintextOutputFn = func(r configResource) string {
	return fmt.Sprintf("%s (%s)", r["name"], r["key"])
}

// TODO: rename this to be "cleaner"? -- NewSingularOutput()
func NewSingularOutputterFn(input []byte) singularOutputterFn {
	return singularOutputterFn{
		input: input,
	}
}

type singularOutputterFn struct {
	input []byte
}

func (o singularOutputterFn) New() (Outputter, error) {
	var r configResource
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return SingularOutputter{}, err
	}

	return SingularOutputter{
		outputFn:     singularPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}

type SingularOutputter struct {
	outputFn     PlaintextOutputFn[configResource]
	resource     configResource
	resourceJSON []byte
}

func (o SingularOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o SingularOutputter) String() string {
	return formatColl([]configResource{o.resource}, o.outputFn)
}
