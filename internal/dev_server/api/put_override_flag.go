package api

import (
	"context"

	_ "github.com/oapi-codegen/runtime"
	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) PutOverrideFlag(ctx context.Context, request PutOverrideFlagRequestObject) (PutOverrideFlagResponseObject, error) {
	if request.Body == nil {
		return nil, errors.New("empty override body")
	}
	override, err := model.UpsertOverride(ctx, request.ProjectKey, request.FlagKey, *request.Body)
	if err != nil {
		if errors.As(err, &model.ErrNotFound{}) {
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
