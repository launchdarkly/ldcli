package flags

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/errors"
)

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key, projKey string) ([]byte, error)
	Update(
		ctx context.Context,
		accessToken,
		baseURI,
		key,
		projKey string,
		patch []ldapi.PatchOperation,
	) ([]byte, error)
}

type FlagsClient struct{}

var _ Client = FlagsClient{}

func NewClient() FlagsClient {
	return FlagsClient{}
}

func (c FlagsClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key,
	projectKey string,
) ([]byte, error) {
	client := c.client(accessToken, baseURI)
	post := ldapi.NewFeatureFlagBody(name, key)
	flag, _, err := client.FeatureFlagsApi.PostFeatureFlag(ctx, projectKey).FeatureFlagBody(*post).Execute()
	if err != nil {
		return nil, errors.NewAPIError(err)

	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func (c FlagsClient) Update(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projKey string,
	patch []ldapi.PatchOperation,
) ([]byte, error) {
	client := c.client(accessToken, baseURI)
	flag, _, err := client.FeatureFlagsApi.
		PatchFeatureFlag(ctx, projKey, key).
		PatchWithComment(*ldapi.NewPatchWithComment(patch)).
		Execute()
	if err != nil {
		return nil, errors.NewAPIError(err)
	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}

// client creates an LD API client. It's not set as a field on the struct because the CLI flags
// are evaluated when running the command, not when executing the program. That means we don't have
// the flag values until the command's RunE method is called.
func (c FlagsClient) client(accessToken string, baseURI string) *ldapi.APIClient {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", accessToken)
	config.Servers[0].URL = baseURI

	return ldapi.NewAPIClient(config)
}
