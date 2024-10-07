package adapters

import (
	"context"
	"fmt"
	"log"

	"github.com/launchdarkly/ldcli/internal/dev_server/adapters/internal"
	"github.com/pkg/errors"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

const ctxKeyApi = ctxKey("adapters.api")

func WithApi(ctx context.Context, s Api) context.Context {
	return context.WithValue(ctx, ctxKeyApi, s)
}

func GetApi(ctx context.Context) Api {
	return ctx.Value(ctxKeyApi).(Api)
}

//go:generate go run go.uber.org/mock/mockgen -destination mocks/api.go -package mocks . Api
type Api interface {
	GetSdkKey(ctx context.Context, projectKey, environmentKey string) (string, error)
	GetAllFlags(ctx context.Context, projectKey string) ([]ldapi.FeatureFlag, error)
	GetProjectEnvironments(ctx context.Context, projectKey string, query string, limit *int) ([]ldapi.Environment, error)
}

type apiClientApi struct {
	apiClient ldapi.APIClient
}

func NewApi(client ldapi.APIClient) Api {
	return apiClientApi{client}
}

func (a apiClientApi) GetSdkKey(ctx context.Context, projectKey, environmentKey string) (string, error) {
	log.Printf("GetSdkKey - projectKey: %s, environmentKey: %s", projectKey, environmentKey)
	environment, _, err := a.apiClient.EnvironmentsApi.GetEnvironment(ctx, projectKey, environmentKey).Execute()
	if err != nil {
		return "", errors.Wrap(err, "unable to get SDK key from LD API")
	}
	return environment.ApiKey, nil
}

func (a apiClientApi) GetAllFlags(ctx context.Context, projectKey string) ([]ldapi.FeatureFlag, error) {
	log.Printf("Fetching all flags for project '%s'", projectKey)
	flags, err := a.getFlags(ctx, projectKey, nil)
	if err != nil {
		err = errors.Wrap(err, "unable to get all flags from LD API")
	}
	return flags, err
}

func (a apiClientApi) GetProjectEnvironments(ctx context.Context, projectKey string, query string, limit *int) ([]ldapi.Environment, error) {
	log.Printf("Fetching all environments for project '%s'", projectKey)
	environments, err := a.getEnvironments(ctx, projectKey, nil, query, limit)
	if err != nil {
		err = errors.Wrap(err, "unable to get environments from LD API")
	}
	return environments, err
}

func (a apiClientApi) getFlags(ctx context.Context, projectKey string, href *string) ([]ldapi.FeatureFlag, error) {
	return internal.GetPaginatedItems(ctx, projectKey, href, func(ctx context.Context, projectKey string, limit, offset *int64) (flags *ldapi.FeatureFlags, err error) {
		// loop until we do not get rate limited
		query := a.apiClient.FeatureFlagsApi.GetFeatureFlags(ctx, projectKey).Limit(100)

		if limit != nil {
			query = query.Limit(*limit)
		}

		if offset != nil {
			query = query.Offset(*offset)
		}
		return internal.Retry429s(query.Execute)
	})
}

func (a apiClientApi) getEnvironments(ctx context.Context, projectKey string, href *string, query string, limit *int) ([]ldapi.Environment, error) {
	request := a.apiClient.EnvironmentsApi.GetEnvironmentsByProject(ctx, projectKey)

	if limit != nil {
		request = request.Limit(int64(*limit))
	}

	if query != "" {
		request = request.Sort("name").Filter(fmt.Sprintf("query:%s", query))
	}

	envs, _, err := request.
		Execute()
	if err != nil {
		return nil, err
	}

	if envs == nil {
		return []ldapi.Environment{}, nil
	}

	return envs.Items, nil
}
