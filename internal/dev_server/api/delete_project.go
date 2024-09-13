package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

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
