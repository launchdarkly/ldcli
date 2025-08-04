package sdk

import (
	"github.com/pkg/errors"
	"io"
	"log"
	"net/http"
)

func SdkEventsReceiveHandler(writer http.ResponseWriter, request *http.Request) {
	log.Println(request.URL.Path)
	bodyStr, err := io.ReadAll(request.Body)
	if err != nil {
		log.Printf("SdkEventsReceiveHandler: error reading request body: %v", err)
		return
	}
	log.Println(string(bodyStr))

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
}

func SdkEventsTeeHandler(writer http.ResponseWriter, request *http.Request) {
	// Initialize SSE
	updateChan, errChan := OpenStream(
		writer,
		request.Context().Done(),
		Message{Event: TYPE_PUT, Data: []byte("start")},
	)
	defer close(updateChan)

	// Use updateChan to continually send messages back to the client. OpenStream, above,
	// takes care of flushing the data.
	//
	// If the client cancels the request, OpenStream will notice via request.Context.Done().
	// Otherwise, this connection is never explicitly closed by us.
	updateChan <- Message{Event: TYPE_PUT, Data: []byte("data1")}
	updateChan <- Message{Event: TYPE_PUT, Data: []byte("data2")}

	err := <-errChan
	if err != nil {
		WriteError(request.Context(), writer, errors.Wrap(err, "stream failure"))
		return
	}
}
