package adapters

import (
	"context"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

const ctxKeyApi = ctxKey("adapters.api")

func WithApi(ctx context.Context, s Api) context.Context {
	return context.WithValue(ctx, ctxKeyApi, s)
}

func GetApi(ctx context.Context) Api {
	return ctx.Value(ctxKeyApi).(Api)
}

type Api struct {
	apiClient ldapi.APIClient
}

func NewApi(client ldapi.APIClient) Api {
	return Api{client}
}

func (a Api) GetSdkKey(ctx context.Context, projectKey, environmentKey string) (string, error) {
	environment, _, err := a.apiClient.EnvironmentsApi.GetEnvironment(ctx, projectKey, environmentKey).Execute()
	if err != nil {
		return "", err
	}
	return environment.ApiKey, nil
}
