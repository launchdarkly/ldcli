package sdk

import (
	"context"
	"net/http"
	"strings"

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
			projectKey, ok := mux.Vars(request)[pathParameter]
			if !ok {
				http.Error(writer, "project key not on path", http.StatusNotFound)
				return
			}
			ctx := request.Context()
			ctx = SetProjectKeyOnContext(ctx, projectKey)
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}

func GetProjectKeyFromAuthorizationHeader(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		ctx := request.Context()
		projectKey := request.Header.Get("Authorization")
		projectKey = strings.TrimPrefix(projectKey, "api_key ") // some sdks set this as a prefix
		if projectKey == "" {
			http.Error(writer, "project key not on Authorization header", http.StatusUnauthorized)
			return
		}
		ctx = SetProjectKeyOnContext(ctx, projectKey)
		request = request.WithContext(ctx)
		handler.ServeHTTP(writer, request)
	})
}
