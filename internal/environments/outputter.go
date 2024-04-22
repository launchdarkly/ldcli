package environments

import (
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/output"
)

type EnvironmentOutputter struct {
	environment *ldapi.Environment
}

func (o EnvironmentOutputter) JSON() (string, error) {
	responseJSON, err := json.Marshal(o.environment)
	if err != nil {
		return "", err
	}

	return string(responseJSON), nil
}

func (o EnvironmentOutputter) String() string {
	fnPlaintext := func(p *ldapi.Environment) string {
		return fmt.Sprintf("%s (%s)", p.Name, p.Key)
	}

	return output.FormatColl([]*ldapi.Environment{o.environment}, fnPlaintext)
}

func NewEnvironmentOutputter(environment *ldapi.Environment) EnvironmentOutputter {
	return EnvironmentOutputter{
		environment: environment,
	}
}
