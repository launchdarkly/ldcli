package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) DeleteDebugSession(ctx context.Context, request DeleteDebugSessionRequestObject) (DeleteDebugSessionResponseObject, error) {
	eventStore := model.EventStoreFromContext(ctx)
	if eventStore == nil {
		return DeleteDebugSession404JSONResponse{ErrorResponseJSONResponse{
			Code:    "event_store_not_found",
			Message: "Event store not available",
		}}, nil
	}

	// Delete the debug session
	err := eventStore.DeleteDebugSession(ctx, request.DebugSessionKey)
	if err != nil {
		return nil, err
	}

	return DeleteDebugSession204Response{}, nil
}
