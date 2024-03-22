package flags

import (
	"context"
	"encoding/json"
	"ldcli/internal/errors"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

type Client interface {
	Create(ctx context.Context, name string, key string, projectKey string) ([]byte, error)
	Update(ctx context.Context, key string, projKey string) ([]byte, error)
}

type FlagsClient struct {
	client *ldapi.APIClient
}

func NewClient(accessToken string, baseURI string) FlagsClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI
	client := ldapi.NewAPIClient(config)

	return FlagsClient{
		client: client,
	}
}

func (c FlagsClient) Create(
	ctx context.Context,
	name string,
	key string,
	projectKey string,
) ([]byte, error) {
	post := ldapi.NewFeatureFlagBody(name, key)
	flag, _, err := c.client.FeatureFlagsApi.PostFeatureFlag(ctx, projectKey).FeatureFlagBody(*post).Execute()
	if err != nil {
		switch err.Error() {
		case "401 Unauthorized":
			return nil, errors.ErrUnauthorized
		case "403 Forbidden":
			return nil, errors.ErrForbidden
		default:
			return nil, err
		}
	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func (c FlagsClient) Update(
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
