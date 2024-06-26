package sdk

import "net/http"

func CorsHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
		writer.Header().Set("Access-Control-Allow-Headers", "Cache-Control,Content-Type,Content-Length,Accept-Encoding,X-LaunchDarkly-User-Agent,X-LaunchDarkly-Payload-ID,X-LaunchDarkly-Wrapper,X-LaunchDarkly-Event-Schema,X-LaunchDarkly-Tags")
		handler.ServeHTTP(writer, request)
	})
}
