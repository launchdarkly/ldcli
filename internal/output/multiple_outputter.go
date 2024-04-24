package output

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
