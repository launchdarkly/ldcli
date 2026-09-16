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
		projectKey := projectKeyFromAuthorizationHeader(request)
		if projectKey == "" {
			http.Error(writer, "project key not on Authorization header", http.StatusUnauthorized)
			return
		}
		ctx = SetProjectKeyOnContext(ctx, projectKey)
		request = request.WithContext(ctx)
		handler.ServeHTTP(writer, request)
	})
}

// GetProjectKeyFromClientCredential resolves the project key from the credential a
// client-side SDK sends to an FDv2 endpoint. Those SDKs authenticate with either a
// mobile key or a client-side ID, and send it either on the Authorization header
// (mobile) or in the auth query parameter (browser). Per the protocol spec the
// header wins when both are present.
//
// The dev server has no real credentials — whichever value arrives is taken to be
// the key of the project to serve.
func GetProjectKeyFromClientCredential(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		projectKey := projectKeyFromAuthorizationHeader(request)
		if projectKey == "" {
			projectKey = request.URL.Query().Get("auth")
		}
		if projectKey == "" {
			http.Error(writer, "project key not on Authorization header or auth query parameter", http.StatusUnauthorized)
			return
		}
		request = request.WithContext(SetProjectKeyOnContext(request.Context(), projectKey))
		handler.ServeHTTP(writer, request)
	})
}

func projectKeyFromAuthorizationHeader(request *http.Request) string {
	projectKey := request.Header.Get("Authorization")
	return strings.TrimPrefix(projectKey, "api_key ") // some sdks set this as a prefix
}
