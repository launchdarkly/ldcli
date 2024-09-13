package api

import (
	"context"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) PostAddProject(ctx context.Context, request PostAddProjectRequestObject) (PostAddProjectResponseObject, error) {
	if request.Body.SourceEnvironmentKey == "" {
		return PostAddProject400JSONResponse{
			ErrorResponseJSONResponse{
				Code:    "invalid_request",
				Message: "sourceEnvironmentKey is required",
			},
		}, nil
	}

	store := model.StoreFromContext(ctx)
	project, err := model.CreateProject(ctx, request.ProjectKey, request.Body.SourceEnvironmentKey, request.Body.Context)
	switch {
	case errors.Is(err, model.ErrAlreadyExists):
		return PostAddProject409JSONResponse{
			Code:    "conflict",
			Message: "project already exists",
		}, nil
	case err != nil:
		return nil, err
	}

	response := ProjectJSONResponse{
		LastSyncedFromSource: project.LastSyncTime.Unix(),
		Context:              project.Context,
		SourceEnvironmentKey: project.SourceEnvironmentKey,
		FlagsState:           &project.AllFlagsState,
	}

	if request.Params.Expand != nil {
		for _, item := range *request.Params.Expand {
			if item == "overrides" {
				overrides, err := store.GetOverridesForProject(ctx, request.ProjectKey)
				if err != nil {
					return nil, err
				}
				respOverrides := make(model.FlagsState)
				for _, override := range overrides {
					if !override.Active {
						continue
					}
					respOverrides[override.FlagKey] = model.FlagState{
						Value:   override.Value,
						Version: override.Version,
					}
				}
				response.Overrides = &respOverrides
			}
			if item == "availableVariations" {
				availableVariations, err := store.GetAvailableVariationsForProject(ctx, request.ProjectKey)
				if err != nil {
					return nil, err
				}
				respAvailableVariations := availableVariationsToResponseFormat(availableVariations)
				response.AvailableVariations = &respAvailableVariations
			}
		}

	}

	return PostAddProject201JSONResponse{
		response,
	}, nil
}
