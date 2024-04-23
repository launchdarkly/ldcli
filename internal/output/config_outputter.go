package output

import (
	"encoding/json"
	"fmt"
	"strings"
)

var configPlaintextOutputFn = func(r configResource) string {
	lst := make([]string, 0)
	for k, v := range r {
		lst = append(lst, fmt.Sprintf("%s: %s", k, v))
	}

	return strings.Join(lst, "\n")
}

func NewConfigOutputterFn(input []byte) configOutputterFn {
	return configOutputterFn{
		input: input,
	}
}

type configOutputterFn struct {
	input []byte
}

func (o configOutputterFn) New() (Outputter, error) {
	var r configResource
	err := json.Unmarshal(o.input, &r)
	if err != nil {
		return ConfigOutputter{}, err
	}

	return ConfigOutputter{
		outputFn:     configPlaintextOutputFn,
		resource:     r,
		resourceJSON: o.input,
	}, nil
}

type ConfigOutputter struct {
	outputFn     PlaintextOutputFn[configResource]
	resource     configResource
	resourceJSON []byte
}

func (o ConfigOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o ConfigOutputter) String() string {
	return formatColl([]configResource{o.resource}, o.outputFn)
}
