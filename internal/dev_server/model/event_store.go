package model

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type eventStoreKey string

const ctxKeyEventStore = ctxKey("model.EventStore")

//go:generate go run go.uber.org/mock/mockgen -destination mocks/event_store.go -package mocks . EventStore

// Event represents a stored event with metadata
type Event struct {
	ID        int64           `json:"id"`
	WrittenAt time.Time       `json:"written_at"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
}

// EventsPage represents a paginated response of events
type EventsPage struct {
	Events     []Event `json:"events"`
	TotalCount int64   `json:"total_count"`
	HasMore    bool    `json:"has_more"`
}

// DebugSession represents a debug session with metadata
type DebugSession struct {
	Key        string    `json:"key"`
	WrittenAt  time.Time `json:"written_at"`
	EventCount int64     `json:"event_count"`
}

// DebugSessionsPage represents a paginated response of debug sessions
type DebugSessionsPage struct {
	Sessions   []DebugSession `json:"sessions"`
	TotalCount int64          `json:"total_count"`
	HasMore    bool           `json:"has_more"`
}

type EventStore interface {
	CreateDebugSession(ctx context.Context, debugSessionKey string) error
	WriteEvent(ctx context.Context, debugSessionKey string, kind string, data json.RawMessage) error
	QueryEvents(ctx context.Context, debugSessionKey string, kind *string, limit int, offset int) (*EventsPage, error)
	QueryDebugSessions(ctx context.Context, limit int, offset int) (*DebugSessionsPage, error)
}

func ContextWithEventStore(ctx context.Context, store EventStore) context.Context {
	return context.WithValue(ctx, ctxKeyEventStore, store)
}

func EventStoreFromContext(ctx context.Context) EventStore {
	return ctx.Value(ctxKeyEventStore).(EventStore)
}

func EventStoreMiddleware(store EventStore) mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			ctx = ContextWithEventStore(ctx, store)
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}
