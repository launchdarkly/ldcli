package sdk

import (
	"net/http"

	"github.com/gorilla/handlers"
)

func CorsHeadersForMethods(methods ...string) func(http.Handler) http.Handler {
	return handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods(methods),
		handlers.AllowCredentials(),
		handlers.ExposedHeaders([]string{"Date"}),
		handlers.AllowedHeaders([]string{"Cache-Control", "Content-Type", "Content-Length", "Accept-Encoding", "X-LaunchDarkly-Event-Schema", "X-LaunchDarkly-User-Agent", "X-LaunchDarkly-Payload-ID", "X-LaunchDarkly-Wrapper", "X-LaunchDarkly-Tags"}),
		handlers.MaxAge(300),
	)
}

var EventsCorsHeaders = handlers.CORS(
	handlers.AllowedOrigins([]string{"*"}),
	handlers.AllowedMethods([]string{"POST"}),
	handlers.AllowCredentials(),
	handlers.AllowedHeaders([]string{"Accept", "Content-Type", "Content-Length", "Accept-Encoding", "X-LaunchDarkly-Event-Schema", "X-LaunchDarkly-User-Agent", "X-LaunchDarkly-Payload-ID", "X-LaunchDarkly-Wrapper", "X-LaunchDarkly-Tags"}),
	handlers.ExposedHeaders([]string{"Date"}),
	handlers.MaxAge(300),
)
