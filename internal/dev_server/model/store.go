package model

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

const ctxKeyStore = "model.Store"

type Store interface {
	GetDevProjects(ctx context.Context) ([]string, error)
	InsertProject(ctx context.Context, project Project) error
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