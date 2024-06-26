package api

import (
	"context"
	"errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

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
		return DeleteDevProjectsProjectKey404Response{}, nil
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

	return GetDevProjectsProjectKey200JSONResponse{
		ProjectJSONResponse{
			LastSyncedFromSource: project.LastSyncTime.Unix(),
			Context:              project.Context,
			SourceEnvironmentKey: project.SourceEnvironmentKey,
		},
	}, nil
}

func (s Server) PostDevProjectsProjectKey(ctx context.Context, request PostDevProjectsProjectKeyRequestObject) (PostDevProjectsProjectKeyResponseObject, error) {
	project, err := model.CreateProject(ctx, request.ProjectKey, request.Body.SourceEnvironmentKey, nil)
	if err != nil {
		return nil, err
	}
	return PostDevProjectsProjectKey201JSONResponse{
		ProjectJSONResponse{
			LastSyncedFromSource: project.LastSyncTime.Unix(),
			Context:              project.Context,
			SourceEnvironmentKey: project.SourceEnvironmentKey,
		},
	}, nil
}

func (s Server) DeleteDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request DeleteDevProjectsProjectKeyOverridesFlagKeyRequestObject) (DeleteDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	store := model.StoreFromContext(ctx)
	err := store.DeleteOverride(ctx, request.ProjectKey, request.FlagKey)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return DeleteDevProjectsProjectKeyOverridesFlagKey404Response{}, nil
		}
		return nil, err
	}
	return DeleteDevProjectsProjectKeyOverridesFlagKey204Response{}, nil
}

func (s Server) PutDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request PutDevProjectsProjectKeyOverridesFlagKeyRequestObject) (PutDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	override, err := model.UpsertOverride(ctx, request.ProjectKey, request.FlagKey, request.Body.String())
	if err != nil {
		return nil, err
	}
	return PutDevProjectsProjectKeyOverridesFlagKey200JSONResponse{FlagOverrideJSONResponse{
		Override: override.Active,
		Value:    override.Value,
	}}, nil
}
