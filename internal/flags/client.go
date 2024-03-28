package flags

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
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

type FlagsClient struct {
	cliVersion string
}

var _ Client = FlagsClient{}

func NewClient(cliVersion string) FlagsClient {
	return FlagsClient{
		cliVersion: cliVersion,
	}
}

func (c FlagsClient) Create(
	ctx context.Context,
	accessToken,
	baseURI,
	name,
	key,
	projectKey string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
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
	client := client.New(accessToken, baseURI, c.cliVersion)
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
