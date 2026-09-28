package flags

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"

	"github.com/launchdarkly/ldcli/internal/client"
	"github.com/launchdarkly/ldcli/internal/errors"
)

type UpdateInput struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

// ClientSideAvailability says which SDK kinds may evaluate a flag. The API
// defaults usingEnvironmentId to false, so a flag a browser SDK is meant to read
// has to ask for it explicitly.
type ClientSideAvailability struct {
	UsingEnvironmentID bool
	UsingMobileKey     bool
}

// CreateOption adjusts the flag being created. Callers that pass none get the
// API's own defaults.
type CreateOption func(*createConfig)

type createConfig struct {
	availability *ClientSideAvailability
}

// WithClientSideAvailability makes the new flag available to the given SDK kinds.
func WithClientSideAvailability(a ClientSideAvailability) CreateOption {
	return func(c *createConfig) { c.availability = &a }
}

func resolveCreateOptions(opts []CreateOption) createConfig {
	var cfg createConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

type Client interface {
	Create(ctx context.Context, accessToken, baseURI, name, key, projKey string, opts ...CreateOption) ([]byte, error)
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
	opts ...CreateOption,
) ([]byte, error) {
	client := client.New(accessToken, baseURI, c.cliVersion)
	post := ldapi.NewFeatureFlagBody(name, key)
	if a := resolveCreateOptions(opts).availability; a != nil {
		post.SetClientSideAvailability(*ldapi.NewClientSideAvailabilityPost(a.UsingEnvironmentID, a.UsingMobileKey))
	}
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
