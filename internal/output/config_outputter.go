package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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

func NewConfigOutputterFn(input []byte) configOutputterFn {
	return configOutputterFn{
		input: input,
	}
}

type configOutputterFn struct {
	input []byte
}

func (o configOutputterFn) New() (Outputter, error) {
	var r resource
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
	outputFn     PlaintextOutputFn[resource]
	resource     resource
	resourceJSON []byte
}

func (o ConfigOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o ConfigOutputter) String() string {
	return formatColl([]resource{o.resource}, o.outputFn)
}
