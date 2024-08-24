package api

import (
	"encoding/json"
	"log"
	"net/http"
)

type errorHandler struct {
	code       string
	statusCode int
}

func (eh errorHandler) HandleError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Error while handling request: %+v", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(eh.statusCode)
	err = json.NewEncoder(w).Encode(ErrorResponseJSONResponse{
		Code:    eh.code,
		Message: err.Error(),
	})
	if err != nil {
		log.Printf("Error while writing error response: %+v", err)
	}
}

var RequestErrorHandler = errorHandler{
	code:       "bad_request",
	statusCode: http.StatusBadRequest,
}.HandleError

var ResponseErrorHandler = errorHandler{
	code:       "internal_server_error",
	statusCode: http.StatusInternalServerError,
}.HandleError
