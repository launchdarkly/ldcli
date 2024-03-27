package flags

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

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
	client := client.New(accessToken, baseURI)
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
	client := client.New(accessToken, baseURI)
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

const MaxNameLength = 50

func NameToKey(name string) (string, error) {
	if len(name) < 1 {
		return "", errors.NewError("Name must not be empty.")
	}
	if len(name) > MaxNameLength {
		return "", errors.NewError("Name must be less than 50 characters.")
	}

	invalid := regexp.MustCompile(`(?i)[^a-z0-9-._\s]+`)
	if invalidStr := invalid.FindString(name); strings.TrimSpace(invalidStr) != "" {
		return "", errors.NewError("Name must start with a letter or number and only contain letters, numbers, '.', '_' or '-'.")
	}

	capitalLettersRegexp := regexp.MustCompile("[A-Z]")
	spacesRegexp := regexp.MustCompile(`\s+`)

	key := spacesRegexp.ReplaceAllString(name, "-")
	key = capitalLettersRegexp.ReplaceAllStringFunc(key, func(match string) string {
		return "-" + strings.ToLower(match)
	})
	key = strings.ReplaceAll(key, "--", "-")
	key = strings.TrimPrefix(key, "-")

	return key, nil
}
