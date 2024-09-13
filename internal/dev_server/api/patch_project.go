package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) PatchProject(ctx context.Context, request PatchProjectRequestObject) (PatchProjectResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := model.UpdateProject(ctx, request.ProjectKey, request.Body.Context, request.Body.SourceEnvironmentKey)
	if err != nil {
		return nil, err
	}
	if project.Key == "" && project.SourceEnvironmentKey == "" {
		return PatchProject404Response{}, nil
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

	return PatchProject200JSONResponse{
		response,
	}, nil
}
