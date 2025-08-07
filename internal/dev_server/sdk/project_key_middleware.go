package sdk

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
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
			log.Printf("GetProjectKeyFromEnvIdParameter middleware called: %s %s", request.Method, request.URL.Path)
			projectKey, ok := mux.Vars(request)[pathParameter]
			if !ok {
				log.Printf("project key not found in path for parameter: %s", pathParameter)
				http.Error(writer, "project key not on path", http.StatusNotFound)
				return
			}
			log.Printf("Extracted project key: %s", projectKey)
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

func GetProjectKeyFromClientSideId(pathParameter string) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			log.Printf("GetProjectKeyFromClientSideId middleware called: %s %s", request.Method, request.URL.Path)
			clientSideId, ok := mux.Vars(request)[pathParameter]
			if !ok {
				log.Printf("client-side ID not found in path for parameter: %s", pathParameter)
				http.Error(writer, "client-side ID not on path", http.StatusNotFound)
				return
			}
			log.Printf("Extracted client-side ID: %s", clientSideId)

			ctx := request.Context()
			store := model.StoreFromContext(ctx)

			// Look up project by client-side ID (fast database lookup)
			project, err := store.GetDevProjectByClientSideId(ctx, clientSideId)
			if err != nil {
				log.Printf("No project found for client-side ID %s: %v", clientSideId, err)
				http.Error(writer, fmt.Sprintf("project not found for client-side ID %s", clientSideId), http.StatusNotFound)
				return
			}

			log.Printf("Found project %s for client-side ID %s", project.Key, clientSideId)
			ctx = SetProjectKeyOnContext(ctx, project.Key)
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}
