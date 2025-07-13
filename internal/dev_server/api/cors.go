package api

import (
	"net/http"
)

// CorsHeadersWithConfig provides configurable CORS support for the dev-server admin API endpoints.
// When enabled=false, no CORS headers are added.
// When enabled=true, CORS headers are added with the specified origin.
func CorsHeadersWithConfig(enabled bool, origin string) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if enabled {
				writer.Header().Set("Access-Control-Allow-Origin", origin)
				writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				writer.Header().Set("Access-Control-Allow-Credentials", "true")
				writer.Header().Set("Access-Control-Allow-Headers", "Accept,Content-Type,Content-Length,Accept-Encoding,Authorization,X-Requested-With")
				writer.Header().Set("Access-Control-Expose-Headers", "Date,Content-Length")
				writer.Header().Set("Access-Control-Max-Age", "300")

				// Handle preflight OPTIONS requests
				if request.Method == http.MethodOptions {
					writer.WriteHeader(http.StatusOK)
					return
				}
			}

			handler.ServeHTTP(writer, request)
		})
	}
}
