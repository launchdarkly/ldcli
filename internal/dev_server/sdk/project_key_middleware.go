package sdk

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

type ctxKey string

const projectKeyContextKey = ctxKey("projectKey")

func SetProjectKeyOnContext(ctx context.Context, projectKey string) context.Context {
	return context.WithValue(ctx, projectKeyContextKey, projectKey)
}
func GetProjectKeyFromContext(ctx context.Context) string {
	return ctx.Value(projectKeyContextKey).(string)
}

func GetProjectKeyFromEnvIdParameter(pathParameter string) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			projectKey := mux.Vars(request)[pathParameter]
			ctx := request.Context()
			ctx = SetProjectKeyOnContext(ctx, projectKey)
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}
