package environments

import (
	"encoding/json"
	"fmt"

	"ldcli/internal/output"
)

// resource is the subset of data we need to display a command's plain text response.
type resource struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type EnvironmentOutputter struct {
	resourceJSON []byte
}

func (o EnvironmentOutputter) JSON() string {
	return string(o.resourceJSON)
}

func (o EnvironmentOutputter) String() string {
	var r resource
	_ = json.Unmarshal(o.resourceJSON, &r)
	fnPlaintext := func(p resource) string {
		return fmt.Sprintf("%s (%s)", p.Name, p.Key)
	}

	return output.FormatColl([]resource{r}, fnPlaintext)
}

func NewEnvironmentOutputter(resourceJSON []byte) EnvironmentOutputter {
	return EnvironmentOutputter{
		resourceJSON: resourceJSON,
	}
}
