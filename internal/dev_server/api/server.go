package api

import (
	"context"
	"errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config oapi-codegen-cfg.yaml api.yaml
var _ StrictServerInterface = server{}

type server struct {
}

func NewStrictServer() StrictServerInterface {
	return server{}
}

func (s server) GetProjects(ctx context.Context, request GetProjectsRequestObject) (GetProjectsResponseObject, error) {
	store := model.StoreFromContext(ctx)
	projectKeys, err := store.GetDevProjectKeys(ctx)
	if err != nil {
		return nil, err
	}
	if projectKeys == nil {
		projectKeys = make([]string, 0) // HACK to make the json behavior compatible with go.
	}
	return GetProjects200JSONResponse(projectKeys), nil
}

func (s server) DeleteProject(ctx context.Context, request DeleteProjectRequestObject) (DeleteProjectResponseObject, error) {
	store := model.StoreFromContext(ctx)
	deleted, err := store.DeleteDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return DeleteProject404JSONResponse{ErrorResponseJSONResponse{
			Code:    "not_found",
			Message: "project not found",
		}}, nil
	}
	return DeleteProject204Response{}, nil
}

func (s server) GetProject(ctx context.Context, request GetProjectRequestObject) (GetProjectResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return GetProject404Response{}, nil
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

	return GetProject200JSONResponse{
		response,
	}, nil
}

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

func (s server) PatchSyncProject(ctx context.Context, request PatchSyncProjectRequestObject) (PatchSyncProjectResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := model.UpdateProject(ctx, request.ProjectKey, nil, nil)
	if err != nil {
		return nil, err
	}
	if project.Key == "" && project.SourceEnvironmentKey == "" {
		return PatchSyncProject404Response{}, nil
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

	return PatchSyncProject200JSONResponse{
		response,
	}, nil
}

func (s server) DeleteFlagOverride(ctx context.Context, request DeleteFlagOverrideRequestObject) (DeleteFlagOverrideResponseObject, error) {
	store := model.StoreFromContext(ctx)
	err := store.DeactivateOverride(ctx, request.ProjectKey, request.FlagKey)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return DeleteFlagOverride404Response{}, nil
		}
		return nil, err
	}
	return DeleteFlagOverride204Response{}, nil
}

func (s server) PutOverrideFlag(ctx context.Context, request PutOverrideFlagRequestObject) (PutOverrideFlagResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("empty override body")
	}
	override, err := model.UpsertOverride(ctx, request.ProjectKey, request.FlagKey, *request.Body)
	if err != nil {
		if errors.As(err, &model.Error{}) {
			return PutOverrideFlag400JSONResponse{
				ErrorResponseJSONResponse{
					Code:    "invalid_request",
					Message: err.Error(),
				},
			}, nil
		}
		return nil, err
	}
	return PutOverrideFlag200JSONResponse{FlagOverrideJSONResponse{
		Override: override.Active,
		Value:    override.Value,
	}}, nil
}

func (s server) GetProjectEnvironments(ctx context.Context, request GetProjectEnvironmentsRequestObject) (GetProjectEnvironmentsResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return GetProjectEnvironments404JSONResponse{}, nil
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

	return GetProjectEnvironments200JSONResponse(envReps), nil
}
