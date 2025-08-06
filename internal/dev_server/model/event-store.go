package model

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type eventStoreKey string

const ctxKeyEventStore = ctxKey("model.EventStore")

//go:generate go run go.uber.org/mock/mockgen -destination mocks/store.go -package mocks . EventStore

type EventStore interface {
	WriteEvent(ctx context.Context, kind string, data json.RawMessage) error
}

func ContextWithEventStore(ctx context.Context, store EventStore) context.Context {
	return context.WithValue(ctx, ctxKeyEventStore, store)
}

func EventStoreFromContext(ctx context.Context) Store {
	return ctx.Value(ctxKeyStore).(Store)
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
