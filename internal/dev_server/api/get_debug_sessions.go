package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) GetDebugSessions(ctx context.Context, request GetDebugSessionsRequestObject) (GetDebugSessionsResponseObject, error) {
	eventStore := model.EventStoreFromContext(ctx)

	// Set default values for pagination
	limit := 50
	offset := 0

	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}

	// Validate parameters
	if limit < 1 || limit > 1000 {
		return GetDebugSessions400JSONResponse{ErrorResponseJSONResponse{
			Code:    "invalid_parameter",
			Message: "limit must be between 1 and 1000",
		}}, nil
	}

	if offset < 0 {
		return GetDebugSessions400JSONResponse{ErrorResponseJSONResponse{
			Code:    "invalid_parameter",
			Message: "offset must be non-negative",
		}}, nil
	}

	// Query debug sessions from the event store
	page, err := eventStore.QueryDebugSessions(ctx, limit, offset)
	if err != nil {
		return nil, err
	}

	// Convert model.DebugSession to API DebugSession
	var apiSessions []DebugSession
	for _, session := range page.Sessions {
		apiSessions = append(apiSessions, DebugSession{
			Key:        session.Key,
			WrittenAt:  session.WrittenAt,
			EventCount: session.EventCount,
		})
	}

	response := DebugSessionsPage{
		Sessions:   apiSessions,
		TotalCount: page.TotalCount,
		HasMore:    page.HasMore,
	}

	return GetDebugSessions200JSONResponse(response), nil
}
