package sdk

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func newSdkEventObserver(updateChan chan<- Message, ctx context.Context) sdkEventObserver {
	debugSessionKey := uuid.New().String()
	db := model.EventStoreFromContext(ctx)
	err := db.CreateDebugSession(ctx, debugSessionKey)
	if err != nil {
		log.Printf("sdkEventObserver: error writting debug session: %v", err)
	}
	return sdkEventObserver{
		debugSessionKey: debugSessionKey,
		ctx:             ctx,
		updateChan:      updateChan,
	}
}

type SDKEventBase struct {
	Kind string `json:"kind"`
}

type sdkEventObserver struct {
	ctx             context.Context
	debugSessionKey string
	updateChan      chan<- Message
}

func (o sdkEventObserver) Handle(message interface{}) {
	str, ok := message.(json.RawMessage)
	if !ok {
		return
	}

	event := SDKEventBase{}
	err := json.Unmarshal(str, &event)
	if err != nil {
		log.Printf("sdkEventObserver: error unmarshaling event: %v", err)
		return
	}

	db := model.EventStoreFromContext(o.ctx)

	err = db.WriteEvent(o.ctx, o.debugSessionKey, event.Kind, str)
	if err != nil {
		log.Printf("sdkEventObserver: error writting event: %v", err)
		return
	}

	o.updateChan <- Message{Event: TYPE_PUT, Data: str}
}

var observers *model.Observers = model.NewObservers()

func SdkEventsReceiveHandler(writer http.ResponseWriter, request *http.Request) {
	bodyStr, err := io.ReadAll(request.Body)
	if err != nil {
		log.Printf("SdkEventsReceiveHandler: error reading request body: %v", err)
		return
	}

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

func SdkEventsTeeHandler(writer http.ResponseWriter, request *http.Request) {
	updateChan, errChan := OpenStream(
		writer,
		request.Context().Done(),
		Message{Event: TYPE_PUT, Data: []byte{}},
	)
	defer close(updateChan)

	observerId := observers.RegisterObserver(newSdkEventObserver(updateChan, request.Context()))
	defer func() {
		ok := observers.DeregisterObserver(observerId)
		if !ok {
			log.Printf("unable to remove observer")
		}
	}()

	err := <-errChan
	if err != nil {
		WriteError(request.Context(), writer, errors.Wrap(err, "stream failure"))
		return
	}
}
