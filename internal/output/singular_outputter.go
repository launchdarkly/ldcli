package output

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
