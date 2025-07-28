package api

import (
	"context"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) DeleteOverrides(ctx context.Context, request DeleteOverridesRequestObject) (DeleteOverridesResponseObject, error) {
	err := model.DeleteOverrides(ctx, request.ProjectKey)
	if err != nil {
		if errors.As(err, &model.ErrNotFound{}) {
			return DeleteOverrides404JSONResponse{}, nil
		}
		return nil, err
	}
	return DeleteOverrides204Response{}, nil
}
