package model

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
)

func GetEnvironmentsForProject(ctx context.Context, projectKey string) ([]Environment, error) {
	apiAdapter := adapters.GetApi(ctx)
	environments, err := apiAdapter.GetProjectEnvironments(ctx, projectKey)
	if err != nil {
		return nil, err
	}

	var allEnvironments []Environment
	for _, environment := range environments {
		allEnvironments = append(allEnvironments, Environment{
			Key:  environment.Key,
			Name: environment.Name,
		})
	}

	return allEnvironments, nil
}
