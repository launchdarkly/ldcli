package output

import (
	"encoding/json"
	"fmt"
)

// resource is the subset of data we need to display a command's plain text response.
type resource struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type SingularOutputter struct {
	resourceJSON []byte
}

func (o SingularOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o SingularOutputter) String() string {
	var r resource
	_ = json.Unmarshal(o.resourceJSON, &r)
	fnPlaintext := func(p resource) string {
		return fmt.Sprintf("%s (%s)", p.Name, p.Key)
	}

	return formatColl([]resource{r}, fnPlaintext)
}

func NewSingularOutputter(resourceJSON []byte) SingularOutputter {
	return SingularOutputter{
		resourceJSON: resourceJSON,
	}
}
