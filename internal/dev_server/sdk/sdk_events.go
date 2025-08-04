package sdk

import (
	"io"
	"log"
	"net/http"
)

func SdkEventswHandler(writer http.ResponseWriter, request *http.Request) {
	log.Println(request.URL.Path)
	bodyStr, err := io.ReadAll(request.Body)
	if err != nil {
		log.Printf("SdkEventswHandler: error reading request body: %v", err)
		return
	}
	log.Println(string(bodyStr))

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
}
