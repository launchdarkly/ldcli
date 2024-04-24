package output

import "strings"

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

type SingularOutputter struct {
	outputFn     PlaintextOutputFn
	resource     resource
	resourceJSON []byte
}

func (o SingularOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o SingularOutputter) String() string {
	return formatColl([]resource{o.resource}, o.outputFn)
}

// formatColl applies a formatting function to every element in the collection and returns it as a
// string.
func formatColl[T any](coll []T, formatFn func(T) string) string {
	lst := make([]string, 0, len(coll))
	for _, c := range coll {
		lst = append(lst, formatFn(c))
	}

	return strings.Join(lst, "\n")
}
