package api

import (
	"context"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) GetDebugSessionEvents(ctx context.Context, request GetDebugSessionEventsRequestObject) (GetDebugSessionEventsResponseObject, error) {
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
		return GetDebugSessionEvents400JSONResponse{ErrorResponseJSONResponse{
			Code:    "invalid_parameter",
			Message: "limit must be between 1 and 1000",
		}}, nil
	}

	if offset < 0 {
		return GetDebugSessionEvents400JSONResponse{ErrorResponseJSONResponse{
			Code:    "invalid_parameter",
			Message: "offset must be non-negative",
		}}, nil
	}

	// Query events from the event store
	page, err := eventStore.QueryEvents(ctx, request.DebugSessionKey, request.Params.Kind, limit, offset)
	if err != nil {
		return nil, err
	}

	// Convert model.Event to API Event
	var apiEvents []Event
	for _, event := range page.Events {
		apiEvents = append(apiEvents, Event{
			Id:        event.ID,
			WrittenAt: event.WrittenAt,
			Kind:      event.Kind,
			Data:      event.Data,
		})
	}

	response := EventsPage{
		Events:     apiEvents,
		TotalCount: page.TotalCount,
		HasMore:    page.HasMore,
	}

	return GetDebugSessionEvents200JSONResponse(response), nil
}
