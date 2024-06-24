package adapters

import (
	"context"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/pkg/errors"
)

const ctxKeyApi = "adapters.api"

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
	project, _, err := a.apiClient.ProjectsApi.GetProject(ctx, projectKey).Execute()
	if err != nil {
		return "", err
	}
	for _, environment := range project.Environments.Items {
		if environment.Key == environmentKey {
			return environment.ApiKey, nil
		}
	}
	return "", errors.Errorf("environment, %s, not found", environmentKey)
}
