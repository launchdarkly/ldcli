package events

type Base struct {
	Kind string `json:"kind"`
}

type Filter struct {
	Kind *string
}

func (f Filter) Matches(e Base) bool {
	if f.Kind == nil {
		return true
	}
	return e.Kind == *f.Kind
}
