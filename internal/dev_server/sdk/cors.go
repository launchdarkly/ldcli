package sdk

import "net/http"

// CorsHeaders sets CORS headers for SDK flag evaluation endpoints and
// short-circuits preflight OPTIONS requests with 204 No Content.
func CorsHeaders(handler http.Handler) http.Handler {
    return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.Header().Set("Access-Control-Allow-Origin", "*")
        writer.Header().Set("Access-Control-Allow-Methods", "GET,OPTIONS")
        writer.Header().Set("Access-Control-Allow-Credentials", "true")
        writer.Header().Set("Access-Control-Allow-Headers", "Cache-Control,Content-Type,Content-Length,Accept-Encoding,X-LaunchDarkly-User-Agent,X-LaunchDarkly-Payload-ID,X-LaunchDarkly-Wrapper,X-LaunchDarkly-Event-Schema,X-LaunchDarkly-Tags")
        writer.Header().Set("Access-Control-Expose-Headers", "Date")
        writer.Header().Set("Access-Control-Max-Age", "300")
        if request.Method == http.MethodOptions {
            writer.WriteHeader(http.StatusNoContent)
            return
        }
        handler.ServeHTTP(writer, request)
    })
}

// EventsCorsHeaders sets CORS headers for SDK events endpoints and
// short-circuits preflight OPTIONS requests with 204 No Content.
func EventsCorsHeaders(handler http.Handler) http.Handler {
    return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        writer.Header().Set("Access-Control-Allow-Origin", "*")
        writer.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
        writer.Header().Set("Access-Control-Allow-Credentials", "true")
        writer.Header().Set("Access-Control-Allow-Headers", "Accept,Content-Type,Content-Length,Accept-Encoding,X-LaunchDarkly-Event-Schema,X-LaunchDarkly-User-Agent,X-LaunchDarkly-Payload-ID,X-LaunchDarkly-Wrapper,X-LaunchDarkly-Tags")
        writer.Header().Set("Access-Control-Expose-Headers", "Date")
        writer.Header().Set("Access-Control-Max-Age", "300")
        if request.Method == http.MethodOptions {
            writer.WriteHeader(http.StatusNoContent)
            return
        }
        handler.ServeHTTP(writer, request)
    })
}
