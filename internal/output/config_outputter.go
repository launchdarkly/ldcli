package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// configPlaintextOutputFn converts the resource to plain text specifically for data from the
// config file.
var configPlaintextOutputFn = func(r resource) string {
	keys := make([]string, 0)
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lst := make([]string, 0)
	for _, k := range keys {
		lst = append(lst, fmt.Sprintf("%s: %s", k, r[k]))
	}

	return strings.Join(lst, "\n")
}

type configOutputterFn struct {
	input []byte
}

// New unmarshals a single config resource and wires up a particular plain text output function.
func (o configOutputterFn) New() (Outputter, error) {
	var r resource
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return SingularOutputter{}, err
	}

	return SingularOutputter{
		outputFn:     configPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}

func NewConfigOutput(input []byte) configOutputterFn {
	return configOutputterFn{
		input: input,
	}
}
