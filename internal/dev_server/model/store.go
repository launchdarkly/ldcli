package model

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

type ctxKey string

const ctxKeyStore = ctxKey("model.Store")

type Store interface {
	DeleteOverride(ctx context.Context, projectKey, flagKey string) error
	GetDevProjects(ctx context.Context) ([]string, error)
	// GetDevProject fetches the project based on the projectKey. If it doesn't exist, ErrNotFound is returned
	GetDevProject(ctx context.Context, projectKey string) (*Project, error)
	UpdateProject(ctx context.Context, project Project) (bool, error)
	DeleteDevProject(ctx context.Context, projectKey string) (bool, error)
	InsertProject(ctx context.Context, project Project) error
	UpsertOverride(ctx context.Context, override Override) (Override, error)
	GetOverridesForProject(ctx context.Context, projectKey string) (Overrides, error)
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

var ErrNotFound = errors.New("not found")

type Error struct {
	err     error
	message string
}

func (e Error) Error() string {
	return e.message
}

func (e Error) Unwrap() error {
	return e.err
}

func NewError(message string) error {
	return errors.WithStack(Error{
		err:     errors.New(message),
		message: message,
	})
}
