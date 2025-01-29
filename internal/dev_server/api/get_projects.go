package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) GetProjects(ctx context.Context, _ GetProjectsRequestObject) (GetProjectsResponseObject, error) {
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
