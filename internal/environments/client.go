package environments

import (
	"context"

	"ldcli/internal/client"
	"ldcli/internal/errors"
	"ldcli/internal/output"
)

type Client interface {
	Get(ctx context.Context, accessToken, baseURI, outputKind, key, projKey string) ([]byte, error)
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
	outputKind,
	key,
	projectKey string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	environment, _, err := client.EnvironmentsApi.GetEnvironment(ctx, projectKey, key).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	output, err := output.CmdOutput(outputKind, NewEnvironmentOutputter(environment))
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	return []byte(output), nil
}
