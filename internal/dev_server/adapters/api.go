package adapters

import (
	"context"
	"log"
	"net/url"
	"strconv"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/pkg/errors"
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

func (a apiClientApi) getFlags(ctx context.Context, projectKey string, href *string) ([]ldapi.FeatureFlag, error) {
	var featureFlags *ldapi.FeatureFlags
	var err error
	if href == nil {
		featureFlags, _, err = a.apiClient.FeatureFlagsApi.GetFeatureFlags(ctx, projectKey).
			Summary(false).
			Execute()
		if err != nil {
			return nil, err
		}
	} else {
		limit, offset, err := parseHref(*href)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to parse href for next link: %s", *href)
		}
		featureFlags, _, err = a.apiClient.FeatureFlagsApi.GetFeatureFlags(ctx, projectKey).
			Summary(false).
			Limit(limit).
			Offset(offset).
			Execute()
		if err != nil {
			return nil, err
		}
	}
	flags := featureFlags.Items
	if next, ok := featureFlags.Links["next"]; ok && next.Href != nil {
		newFlags, err := a.getFlags(ctx, projectKey, next.Href)
		if err != nil {
			return nil, err
		}
		flags = append(flags, newFlags...)
	}
	return flags, nil
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
