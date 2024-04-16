package flags

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"ldcli/internal/client"
	"ldcli/internal/errors"
)

type UpdateInput struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key, projKey string) ([]byte, error)
	Get(ctx context.Context, accessToken, baseURI, key, projKey, envKey string) ([]byte, error)
	Update(
		ctx context.Context,
		accessToken,
		baseURI,
		key,
		projKey string,
		patch []UpdateInput,
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
		return nil, errors.NewLDAPIError(err)
	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func (c FlagsClient) Get(
	ctx context.Context,
	accessToken,
	baseURI,
	key,
	projectKey,
	environmentKey string,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	flag, _, err := client.FeatureFlagsApi.GetFeatureFlag(ctx, projectKey, key).Env(environmentKey).Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
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
	input []UpdateInput,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	patch := []ldapi.PatchOperation{}
	for _, i := range input {
		patch = append(patch, *ldapi.NewPatchOperation(i.Op, i.Path, i.Value))
	}
	flag, _, err := client.FeatureFlagsApi.
		PatchFeatureFlag(ctx, projKey, key).
		PatchWithComment(*ldapi.NewPatchWithComment(patch)).
		Execute()
	if err != nil {
		return nil, errors.NewLDAPIError(err)
	}

	responseJSON, err := json.Marshal(flag)
	if err != nil {
		return nil, err
	}

	return responseJSON, nil
}

func BuildToggleFlagPatch(envKey string, enabled bool) []UpdateInput {
	return []UpdateInput{{Op: "replace", Path: fmt.Sprintf("/environments/%s/on", envKey), Value: enabled}}
}
