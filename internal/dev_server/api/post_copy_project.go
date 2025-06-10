package api

import (
	"context"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) PostCopyProject(ctx context.Context, request PostCopyProjectRequestObject) (PostCopyProjectResponseObject, error) {
	if request.Body.NewProjectKey == "" {
		return PostCopyProject400JSONResponse{
			ErrorResponseJSONResponse{
				Code:    "invalid_request",
				Message: "newProjectKey is required",
			},
		}, nil
	}

	store := model.StoreFromContext(ctx)

	// Check if source project exists
	_, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		var notFoundErr model.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return PostCopyProject404JSONResponse{
				Code:    "not_found",
				Message: "source project not found",
			}, nil
		}
		return nil, err
	}

	// Copy the project
	newProject, err := model.CopyProject(ctx, request.ProjectKey, request.Body.NewProjectKey, nil)
	switch {
	case errors.As(err, &model.ErrAlreadyExists{}):
		return PostCopyProject409JSONResponse{
			Code:    "conflict",
			Message: err.Error(),
		}, nil
	case err != nil:
		return nil, err
	}

	response := ProjectJSONResponse{
		LastSyncedFromSource: newProject.LastSyncTime.Unix(),
		Context:              newProject.Context,
		SourceEnvironmentKey: newProject.SourceEnvironmentKey,
		FlagsState:           &newProject.AllFlagsState,
	}

	if request.Params.Expand != nil {
		for _, item := range *request.Params.Expand {
			if item == "overrides" {
				overrides, err := store.GetOverridesForProject(ctx, request.Body.NewProjectKey)
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
				availableVariations, err := store.GetAvailableVariationsForProject(ctx, request.Body.NewProjectKey)
				if err != nil {
					return nil, err
				}
				respAvailableVariations := availableVariationsToResponseFormat(availableVariations)
				response.AvailableVariations = &respAvailableVariations
			}
		}
	}

	return PostCopyProject201JSONResponse{
		response,
	}, nil
}
