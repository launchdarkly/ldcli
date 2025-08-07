package model

import (
	"context"
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

type ctxKey string

const ctxKeyStore = ctxKey("model.Store")

//go:generate go run go.uber.org/mock/mockgen -destination mocks/store.go -package mocks . Store

type Store interface {
	// DeactivateOverride deactivates the override for the flag, returning the updated version of the override.
	// ErrNotFound is returned if there isn't an override for the flag.
	DeactivateOverride(ctx context.Context, projectKey, flagKey string) (int, error)
	GetDevProjectKeys(ctx context.Context) ([]string, error)
	// GetDevProject fetches the project based on the projectKey. If it doesn't exist, ErrNotFound is returned
	GetDevProject(ctx context.Context, projectKey string) (*Project, error)
	// GetDevProjectByClientSideId fetches the project based on the client-side ID. If it doesn't exist, ErrNotFound is returned
	GetDevProjectByClientSideId(ctx context.Context, clientSideId string) (*Project, error)
	UpdateProject(ctx context.Context, project Project) (bool, error)
	DeleteDevProject(ctx context.Context, projectKey string) (bool, error)
	// InsertProject inserts the project. If it already exists, ErrAlreadyExists is returned
	InsertProject(ctx context.Context, project Project) error
	UpsertOverride(ctx context.Context, override Override) (Override, error)
	GetOverridesForProject(ctx context.Context, projectKey string) (Overrides, error)
	GetAvailableVariationsForProject(ctx context.Context, projectKey string) (map[string][]Variation, error)

	CreateBackup(ctx context.Context) (io.ReadCloser, int64, error)
	RestoreBackup(ctx context.Context, stream io.Reader) (string, error)
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
