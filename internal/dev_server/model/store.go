package model

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

type ctxKey string

const ctxKeyStore = ctxKey("model.Store")

type Store interface {
	DeleteOverride(ctx context.Context, projectKey, flagKey string) error
	GetDevProjects(ctx context.Context) ([]string, error)
	InsertProject(ctx context.Context, project Project) error
	UpsertOverride(ctx context.Context, override Override) error
}

func ContextWithStore(ctx context.Context, store Store) context.Context {
	return context.WithValue(ctx, ctxKeyStore, store)
}

func StoreFromContext(ctx context.Context) Store {
	return ctx.Value(ctxKeyStore).(Store)
}

func StoreMiddleware(store Store) mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			ctx = ContextWithStore(ctx, store)
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}
