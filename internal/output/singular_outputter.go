package output

import (
	"encoding/json"
	"fmt"
)

var singularPlaintextOutputFn = func(r resource) string {
	return fmt.Sprintf("%s (%s)", r.Name, r.Key)
}

func SingularOutput(input []byte) OutputterFn {
	return singularOutputterFn{
		input: input,
	}
}

type singularOutputterFn struct {
	input []byte
}

func (o singularOutputterFn) New() (Outputter, error) {
	var r resource
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return Outputter{}, err
	}

	return Outputter{
		outputFn:     singularPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}
