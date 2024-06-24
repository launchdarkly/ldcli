package adapters

import (
	"net/http"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

// Middleware puts adapters on to the context for consumption by other things
func Middleware(client ldapi.APIClient) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			ctx = WithSdk(ctx, newSdk())
			ctx = WithApi(ctx, NewApi(client))
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}
