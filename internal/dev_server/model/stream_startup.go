package model

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

const ctxKeyStreamStartup = ctxKey("model.StreamStartup")

// WithStreamStartup records whether streaming-startup mode is enabled: load flag state off the SDK stream and resolve variation names from REST in the background, rather than blocking startup on the full fetch.
func WithStreamStartup(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, ctxKeyStreamStartup, enabled)
}

// StreamStartupFromContext reports whether streaming-startup mode is enabled (default false).
func StreamStartupFromContext(ctx context.Context) bool {
	enabled, ok := ctx.Value(ctxKeyStreamStartup).(bool)
	return ok && enabled
}

// StreamStartupMiddleware puts the streaming-startup mode on the request context so handlers sync the same way startup does.
func StreamStartupMiddleware(enabled bool) mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			request = request.WithContext(WithStreamStartup(request.Context(), enabled))
			handler.ServeHTTP(writer, request)
		})
	}
}
