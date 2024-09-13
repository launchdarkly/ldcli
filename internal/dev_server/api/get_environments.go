package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) GetEnvironments(ctx context.Context, request GetEnvironmentsRequestObject) (GetEnvironmentsResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return GetEnvironments404JSONResponse{}, nil
	}

	environments, err := model.GetEnvironmentsForProject(ctx, project.Key)
	if err != nil {
		return nil, err
	}

	var envReps []Environment
	for _, env := range environments {
		envReps = append(envReps, Environment{
			Key:  env.Key,
			Name: env.Name,
		})
	}

	return GetEnvironments200JSONResponse(envReps), nil
}
