package environments

import (
	"context"
	"encoding/json"

	"github.com/launchdarkly/ldcli/internal/client"
	"github.com/launchdarkly/ldcli/internal/errors"
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

	output, err := json.Marshal(environment)
	if err != nil {
		return nil, errors.NewLDAPIError(err)

	}

	return output, nil
}
