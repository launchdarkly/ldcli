package flags

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

type Client interface {
	Update(ctx context.Context, key string, projKey string) ([]byte, error)
}

type flagsClient struct {
	client *ldapi.APIClient
}

func NewClient(accessToken string, baseURI string) flagsClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI
	client := ldapi.NewAPIClient(config)

	return flagsClient{
		client: client,
	}
}

func (c flagsClient) Update(
	ctx context.Context,
	key string,
	projKey string,
	patch []ldapi.PatchOperation,
) ([]byte, error) {
	flag, _, err := c.client.FeatureFlagsApi.
		PatchFeatureFlag(ctx, projKey, key).
		PatchWithComment(*ldapi.NewPatchWithComment(patch)).
		Execute()
	if err != nil {
		return nil, err
	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}
