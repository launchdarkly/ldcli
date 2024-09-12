package adapters

import (
	"context"
	"log"
	"net/url"
	"strconv"

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
	GetProjectEnvironments(ctx context.Context, projectKey string) ([]ldapi.Environment, error)
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

func (a apiClientApi) GetProjectEnvironments(ctx context.Context, projectKey string) ([]ldapi.Environment, error) {
	log.Printf("Fetching all environments for project '%s'", projectKey)
	environments, err := a.getEnvironments(ctx, projectKey, nil)
	if err != nil {
		err = errors.Wrap(err, "unable to get environments from LD API")
	}
	return environments, err
}

func (a apiClientApi) getFlags(ctx context.Context, projectKey string, href *string) ([]ldapi.FeatureFlag, error) {
	return getPaginatedItems(ctx, projectKey, href, func(ctx context.Context, projectKey string, limit, offset *int64) (*ldapi.FeatureFlags, error) {
		query := a.apiClient.FeatureFlagsApi.GetFeatureFlags(ctx, projectKey)

		if limit != nil {
			query = query.Limit(*limit)
		}

		if offset != nil {
			query = query.Offset(*offset)
		}

		flags, _, err := query.
			Execute()
		return flags, err
	})
}

func (a apiClientApi) getEnvironments(ctx context.Context, projectKey string, href *string) ([]ldapi.Environment, error) {
	return getPaginatedItems(ctx, projectKey, href, func(ctx context.Context, projectKey string, limit, offset *int64) (*ldapi.Environments, error) {
		request := a.apiClient.EnvironmentsApi.GetEnvironmentsByProject(ctx, projectKey)
		if limit != nil {
			request = request.Limit(*limit)
		}

		if offset != nil {
			request = request.Offset(*offset)
		}

		envs, _, err := request.
			Execute()
		return envs, err
	})
}

func getPaginatedItems[T any, R interface {
	GetItems() []T
	GetLinks() map[string]ldapi.Link
}](ctx context.Context, projectKey string, href *string, fetchFunc func(context.Context, string, *int64, *int64) (R, error)) ([]T, error) {
	var result R
	var err error

	if href == nil {
		result, err = fetchFunc(ctx, projectKey, nil, nil)
		if err != nil {
			return nil, err
		}
	} else {
		limit, offset, err := parseHref(*href)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to parse href for next link: %s", *href)
		}
		result, err = fetchFunc(ctx, projectKey, &limit, &offset)
		if err != nil {
			return nil, err
		}
	}

	items := result.GetItems()

	if links := result.GetLinks(); links != nil {
		if next, ok := links["next"]; ok && next.Href != nil {
			newItems, err := getPaginatedItems(ctx, projectKey, next.Href, fetchFunc)
			if err != nil {
				return nil, err
			}
			items = append(items, newItems...)
		}
	}

	return items, nil
}

func parseHref(href string) (limit, offset int64, err error) {
	parsedUrl, err := url.Parse(href)
	if err != nil {
		return
	}
	l, err := strconv.Atoi(parsedUrl.Query().Get("limit"))
	if err != nil {
		return
	}
	o, err := strconv.Atoi(parsedUrl.Query().Get("offset"))
	if err != nil {
		return
	}

	limit = int64(l)
	offset = int64(o)
	return
}
