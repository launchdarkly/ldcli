package environments

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/cmd/output"
	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type Client interface {
	Get(ctx context.Context, accessToken, baseURI, key, projKey string) ([]byte, error)
}

type EnvironmentsClient struct {
	cliVersion string
}

var _ Client = EnvironmentsClient{}

func NewClient(cliVersion string) EnvironmentsClient {
	return EnvironmentsClient{
		cliVersion: cliVersion,
	}
}

func (c EnvironmentsClient) Get(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projectKey string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	environment, _, err := client.EnvironmentsApi.GetEnvironment(ctx, projectKey, key).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	output, err := output.CmdOutput("", NewEnvironmentOutputter(environment))
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	return []byte(output), nil
}

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
