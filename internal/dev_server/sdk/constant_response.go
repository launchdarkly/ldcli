package sdk

import "net/http"

func ConstantResponseHandler(statusCode int, response string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(statusCode)
		if len(response) > 0 {
			_, _ = writer.Write([]byte(response))
		}
	}
}
