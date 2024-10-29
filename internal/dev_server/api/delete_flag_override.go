package api

import (
	"context"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) DeleteFlagOverride(ctx context.Context, request DeleteFlagOverrideRequestObject) (DeleteFlagOverrideResponseObject, error) {
	err := model.DeleteOverride(ctx, request.ProjectKey, request.FlagKey)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return DeleteFlagOverride404Response{}, nil
		}
		return nil, err
	}
	return DeleteFlagOverride204Response{}, nil
}
