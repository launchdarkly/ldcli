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

func (s server) GetDevProjects(ctx context.Context, request GetDevProjectsRequestObject) (GetDevProjectsResponseObject, error) {
	store := model.StoreFromContext(ctx)
	projectKeys, err := store.GetDevProjectKeys(ctx)
	if err != nil {
		return nil, err
	}
	if projectKeys == nil {
		projectKeys = make([]string, 0) // HACK to make the json behavior compatible with go.
	}
	return GetDevProjects200JSONResponse(projectKeys), nil
}

func (s server) DeleteDevProjectsProjectKey(ctx context.Context, request DeleteDevProjectsProjectKeyRequestObject) (DeleteDevProjectsProjectKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	deleted, err := store.DeleteDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return DeleteDevProjectsProjectKey404JSONResponse{ErrorResponseJSONResponse{
			Code:    "not_found",
			Message: "project not found",
		}}, nil
	}
	return DeleteDevProjectsProjectKey204Response{}, nil
}

func (s server) GetDevProjectsProjectKey(ctx context.Context, request GetDevProjectsProjectKeyRequestObject) (GetDevProjectsProjectKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return GetDevProjectsProjectKey404Response{}, nil
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

	return GetDevProjectsProjectKey200JSONResponse{
		response,
	}, nil
}

func (s server) PostDevProjectsProjectKey(ctx context.Context, request PostDevProjectsProjectKeyRequestObject) (PostDevProjectsProjectKeyResponseObject, error) {
	if request.Body.SourceEnvironmentKey == "" {
		return PostDevProjectsProjectKey400JSONResponse{
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
		return PostDevProjectsProjectKey409JSONResponse{
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

	return PostDevProjectsProjectKey201JSONResponse{
		response,
	}, nil
}

func (s server) PatchDevProjectsProjectKey(ctx context.Context, request PatchDevProjectsProjectKeyRequestObject) (PatchDevProjectsProjectKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := model.UpdateProject(ctx, request.ProjectKey, request.Body.Context, request.Body.SourceEnvironmentKey)
	if err != nil {
		return nil, err
	}
	if project.Key == "" && project.SourceEnvironmentKey == "" {
		return PatchDevProjectsProjectKey404Response{}, nil
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

	return PatchDevProjectsProjectKey200JSONResponse{
		response,
	}, nil
}

func (s server) PatchDevProjectsProjectKeySync(ctx context.Context, request PatchDevProjectsProjectKeySyncRequestObject) (PatchDevProjectsProjectKeySyncResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := model.UpdateProject(ctx, request.ProjectKey, nil, nil)
	if err != nil {
		return nil, err
	}
	if project.Key == "" && project.SourceEnvironmentKey == "" {
		return PatchDevProjectsProjectKeySync404Response{}, nil
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

	return PatchDevProjectsProjectKeySync200JSONResponse{
		response,
	}, nil
}

func (s server) DeleteDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request DeleteDevProjectsProjectKeyOverridesFlagKeyRequestObject) (DeleteDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	err := store.DeactivateOverride(ctx, request.ProjectKey, request.FlagKey)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return DeleteDevProjectsProjectKeyOverridesFlagKey404Response{}, nil
		}
		return nil, err
	}
	return DeleteDevProjectsProjectKeyOverridesFlagKey204Response{}, nil
}

func (s server) PutDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request PutDevProjectsProjectKeyOverridesFlagKeyRequestObject) (PutDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("empty override body")
	}
	override, err := model.UpsertOverride(ctx, request.ProjectKey, request.FlagKey, *request.Body)
	if err != nil {
		if errors.As(err, &model.Error{}) {
			return PutDevProjectsProjectKeyOverridesFlagKey400JSONResponse{
				ErrorResponseJSONResponse{
					Code:    "invalid_request",
					Message: err.Error(),
				},
			}, nil
		}
		return nil, err
	}
	return PutDevProjectsProjectKeyOverridesFlagKey200JSONResponse{FlagOverrideJSONResponse{
		Override: override.Active,
		Value:    override.Value,
	}}, nil
}

func (s server) GetProjectsEnvironments(ctx context.Context, request GetProjectsEnvironmentsRequestObject) (GetProjectsEnvironmentsResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return GetProjectsEnvironments404JSONResponse{}, nil
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

	return GetProjectsEnvironments200JSONResponse(envReps), nil
}
