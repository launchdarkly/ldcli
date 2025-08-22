package sdk

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

type SDKEventBase struct {
	Kind string `json:"kind"`
}

func SdkEventsReceiveHandler(writer http.ResponseWriter, request *http.Request) {
	bodyStr, err := io.ReadAll(request.Body)
	if err != nil {
		log.Printf("SdkEventsReceiveHandler: error reading request body: %v", err)
		return
	}
	observers := model.GetObserversFromContext(request.Context())

	var arr []json.RawMessage
	err = json.Unmarshal(bodyStr, &arr)

	if err != nil {
		log.Printf("SdkEventsReceiveHandler: error unmarshaling request body: %v", err)
	}

	for _, msg := range arr {
		observers.Notify(msg)
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
}
