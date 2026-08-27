package sdk

import (
	"github.com/gorilla/handlers"
)

var CorsHeaders = handlers.CORS(
	handlers.AllowedOrigins([]string{"*"}),
	handlers.AllowedMethods([]string{"GET"}),
	handlers.AllowCredentials(),
	handlers.ExposedHeaders([]string{"Date"}),
	handlers.AllowedHeaders([]string{"Cache-Control", "Content-Type", "Content-Length", "Accept-Encoding", "X-LaunchDarkly-Event-Schema", "X-LaunchDarkly-User-Agent", "X-LaunchDarkly-Payload-ID", "X-LaunchDarkly-Wrapper", "X-LaunchDarkly-Tags"}),
	handlers.MaxAge(300),
)

// ClientFdv2CorsHeaders is the CORS configuration for the client-side FDv2 endpoints.
// Unlike the FDv1 client-side routes these accept POST and REPORT as well as GET, and
// they accept the credential on the Authorization header as well as the auth query
// parameter.
var ClientFdv2CorsHeaders = handlers.CORS(
	handlers.AllowedOrigins([]string{"*"}),
	handlers.AllowedMethods([]string{"GET", "POST", "REPORT"}),
	handlers.AllowCredentials(),
	handlers.ExposedHeaders([]string{"Date"}),
	handlers.AllowedHeaders([]string{"Authorization", "Cache-Control", "Content-Type", "Content-Length", "Accept-Encoding", "X-LaunchDarkly-Event-Schema", "X-LaunchDarkly-User-Agent", "X-LaunchDarkly-Payload-ID", "X-LaunchDarkly-Wrapper", "X-LaunchDarkly-Tags"}),
	handlers.MaxAge(300),
)

var EventsCorsHeaders = handlers.CORS(
	handlers.AllowedOrigins([]string{"*"}),
	handlers.AllowedMethods([]string{"POST"}),
	handlers.AllowCredentials(),
	handlers.AllowedHeaders([]string{"Accept", "Content-Type", "Content-Length", "Accept-Encoding", "X-LaunchDarkly-Event-Schema", "X-LaunchDarkly-User-Agent", "X-LaunchDarkly-Payload-ID", "X-LaunchDarkly-Wrapper", "X-LaunchDarkly-Tags"}),
	handlers.ExposedHeaders([]string{"Date"}),
	handlers.MaxAge(300),
)
