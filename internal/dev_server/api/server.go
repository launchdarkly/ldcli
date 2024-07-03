package api

import (
	"context"
	"errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

//go:generate oapi-codegen -config oapi-codegen-cfg.yaml api.yaml
type Server struct {
}

func NewStrictServer() Server {
	return Server{}
}

func (s Server) GetDevProjects(ctx context.Context, request GetDevProjectsRequestObject) (GetDevProjectsResponseObject, error) {
	store := model.StoreFromContext(ctx)
	projectKeys, err := store.GetDevProjects(ctx)
	if err != nil {
		return nil, err
	}
	if projectKeys == nil {
		projectKeys = make([]string, 0) // HACK to make the json behavior compatible with go.
	}
	return GetDevProjects200JSONResponse(projectKeys), nil
}

func (s Server) DeleteDevProjectsProjectKey(ctx context.Context, request DeleteDevProjectsProjectKeyRequestObject) (DeleteDevProjectsProjectKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	deleted, err := store.DeleteDevProject(ctx, request.ProjectKey)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return DeleteDevProjectsProjectKey404JSONResponse{NotFoundErrorRespJSONResponse{
			Code:    "not_found",
			Message: "project not found",
		}}, nil
	}
	return DeleteDevProjectsProjectKey204Response{}, nil
}

func (s Server) GetDevProjectsProjectKey(ctx context.Context, request GetDevProjectsProjectKeyRequestObject) (GetDevProjectsProjectKeyResponseObject, error) {
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
		FlagsState:           &project.FlagState,
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
		}

	}

	return GetDevProjectsProjectKey200JSONResponse{
		response,
	}, nil
}

func (s Server) PostDevProjectsProjectKey(ctx context.Context, request PostDevProjectsProjectKeyRequestObject) (PostDevProjectsProjectKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := model.CreateProject(ctx, request.ProjectKey, request.Body.SourceEnvironmentKey, request.Body.Context)
	if err != nil {
		return nil, err
	}

	response := ProjectJSONResponse{
		LastSyncedFromSource: project.LastSyncTime.Unix(),
		Context:              project.Context,
		SourceEnvironmentKey: project.SourceEnvironmentKey,
		FlagsState:           &project.FlagState,
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
		}

	}

	return PostDevProjectsProjectKey201JSONResponse{
		response,
	}, nil
}

func (s Server) PatchDevProjectsProjectKey(ctx context.Context, request PatchDevProjectsProjectKeyRequestObject) (PatchDevProjectsProjectKeyResponseObject, error) {
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
		FlagsState:           &project.FlagState,
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
		}

	}

	return PatchDevProjectsProjectKey200JSONResponse{
		response,
	}, nil
}

func (s Server) PatchDevProjectsProjectKeySync(ctx context.Context, request PatchDevProjectsProjectKeySyncRequestObject) (PatchDevProjectsProjectKeySyncResponseObject, error) {
	store := model.StoreFromContext(ctx)
	project, err := model.SyncProject(ctx, request.ProjectKey)
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
		FlagsState:           &project.FlagState,
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
		}

	}

	return PatchDevProjectsProjectKeySync200JSONResponse{
		response,
	}, nil
}

func (s Server) DeleteDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request DeleteDevProjectsProjectKeyOverridesFlagKeyRequestObject) (DeleteDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	err := store.DeleteOverride(ctx, request.ProjectKey, request.FlagKey)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return DeleteDevProjectsProjectKeyOverridesFlagKey404Response{}, nil
		}
		return nil, err
	}
	return DeleteDevProjectsProjectKeyOverridesFlagKey204Response{}, nil
}

func (s Server) PutDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request PutDevProjectsProjectKeyOverridesFlagKeyRequestObject) (PutDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	override, err := model.UpsertOverride(ctx, request.ProjectKey, request.FlagKey, request.Body.String())
	if err != nil {
		if errors.As(err, &model.Error{}) {
			return PutDevProjectsProjectKeyOverridesFlagKey400JSONResponse{
				InvalidRequestResponseJSONResponse{
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
