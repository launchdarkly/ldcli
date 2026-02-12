package api

import (
	"context"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) PostImportProject(ctx context.Context, request PostImportProjectRequestObject) (PostImportProjectResponseObject, error) {
	// Validate required fields
	if request.Body.SourceEnvironmentKey == "" {
		return PostImportProject400JSONResponse{
			ErrorResponseJSONResponse{
				Code:    "invalid_request",
				Message: "sourceEnvironmentKey is required",
			},
		}, nil
	}

	if len(request.Body.FlagsState) == 0 {
		return PostImportProject400JSONResponse{
			ErrorResponseJSONResponse{
				Code:    "invalid_request",
				Message: "flagsState is required",
			},
		}, nil
	}

	// Build import data from request
	importData := model.ImportData{
		Context:              request.Body.Context,
		SourceEnvironmentKey: request.Body.SourceEnvironmentKey,
		FlagsState:           request.Body.FlagsState,
		Overrides:            request.Body.Overrides,
	}

	// Convert availableVariations if present
	if request.Body.AvailableVariations != nil {
		variations := make(map[string][]model.ImportVariation)
		for flagKey, vars := range *request.Body.AvailableVariations {
			for _, v := range vars {
				variations[flagKey] = append(variations[flagKey], model.ImportVariation{
					Id:          v.Id,
					Name:        v.Name,
					Description: v.Description,
					Value:       v.Value,
				})
			}
		}
		importData.AvailableVariations = &variations
	}

	// Import the project
	err := model.ImportProject(ctx, request.ProjectKey, importData)
	switch {
	case errors.As(err, &model.ErrAlreadyExists{}):
		return PostImportProject409JSONResponse{
			Code:    "conflict",
			Message: err.Error(),
		}, nil
	case err != nil:
		return nil, err
	}

	// Fetch the imported project to return
	store := model.StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}

	response := ProjectJSONResponse{
		LastSyncedFromSource: project.LastSyncTime.Unix(),
		Context:              project.Context,
		SourceEnvironmentKey: project.SourceEnvironmentKey,
		FlagsState:           &project.AllFlagsState,
	}

	return PostImportProject201JSONResponse{response}, nil
}
