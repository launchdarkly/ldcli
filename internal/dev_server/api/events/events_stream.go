package events

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/sdk"
)

type sdkEventObserver struct {
	ctx             context.Context
	debugSessionKey string
	updateChan      chan<- sdk.Message
}

func newSdkEventObserver(updateChan chan<- sdk.Message, ctx context.Context) sdkEventObserver {
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

func (o sdkEventObserver) Handle(message interface{}) {
	str, ok := message.(json.RawMessage)
	if !ok {
		return
	}

	event := sdk.SDKEventBase{}
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

	o.updateChan <- sdk.Message{Event: sdk.TYPE_PUT, Data: str}
}

func SdkEventsTeeHandler(writer http.ResponseWriter, request *http.Request) {
	updateChan, errChan := sdk.OpenStream(
		writer,
		request.Context().Done(),
		sdk.Message{Event: sdk.TYPE_PUT, Data: []byte{}},
	)
	defer close(updateChan)
	observers := model.GetObserversFromContext(request.Context())

	observerId := observers.RegisterObserver(newSdkEventObserver(updateChan, request.Context()))
	defer func() {
		ok := observers.DeregisterObserver(observerId)
		if !ok {
			log.Printf("unable to remove observer")
		}
	}()

	err := <-errChan
	if err != nil {
		sdk.WriteError(request.Context(), writer, errors.Wrap(err, "stream failure"))
		return
	}
}
