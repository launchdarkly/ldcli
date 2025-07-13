package sdk

import "net/http"

func CorsHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
		writer.Header().Set("Access-Control-Allow-Credentials", "true")
		writer.Header().Set("Access-Control-Allow-Headers", "Cache-Control,Content-Type,Content-Length,Accept-Encoding,X-LaunchDarkly-User-Agent,X-LaunchDarkly-Payload-ID,X-LaunchDarkly-Wrapper,X-LaunchDarkly-Event-Schema,X-LaunchDarkly-Tags")
		writer.Header().Set("Access-Control-Expose-Headers", "Date")
		writer.Header().Set("Access-Control-Max-Age", "300")
		handler.ServeHTTP(writer, request)
	})
}

func EventsCorsHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
		writer.Header().Set("Access-Control-Allow-Credentials", "true")
		writer.Header().Set("Access-Control-Allow-Headers", "Accept,Content-Type,Content-Length,Accept-Encoding,X-LaunchDarkly-Event-Schema,X-LaunchDarkly-User-Agent,X-LaunchDarkly-Payload-ID,X-LaunchDarkly-Wrapper,X-LaunchDarkly-Tags")
		writer.Header().Set("Access-Control-Expose-Headers", "Date")
		writer.Header().Set("Access-Control-Max-Age", "300")
		handler.ServeHTTP(writer, request)
	})
}

// ApiCorsHeadersWithConfig provides configurable CORS support for the dev-server API endpoints
func ApiCorsHeadersWithConfig(enabled bool, origin string) func(http.Handler) http.Handler {
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
