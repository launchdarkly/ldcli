package adapters

import (
	"net/http"
	"time"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

type ctxKey string

// Middleware puts adapters on to the context for consumption by other things
func Middleware(client ldapi.APIClient, streamingUrl string, sdkInitTimeout time.Duration) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ctx := request.Context()
			ctx = WithApiAndSdk(ctx, client, streamingUrl, sdkInitTimeout)
			request = request.WithContext(ctx)
			handler.ServeHTTP(writer, request)
		})
	}
}
