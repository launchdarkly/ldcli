package sdk

import (
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

type sdkEventObserver struct {
	updateChan chan<- Message
}

func (o sdkEventObserver) Handle(message interface{}) {
	str, ok := message.(string)
	if !ok {
		return
	}
	o.updateChan <- Message{Event: TYPE_PUT, Data: []byte(str)}
}

var observers *model.Observers
var once sync.Once

func SdkEventsReceiveHandler(writer http.ResponseWriter, request *http.Request) {
	once.Do(func() {
		observers = model.NewObservers()
	})

	bodyStr, err := io.ReadAll(request.Body)
	if err != nil {
		log.Printf("SdkEventsReceiveHandler: error reading request body: %v", err)
		return
	}
	if observers != nil {
		observers.Notify(string(bodyStr))
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusAccepted)
}

func SdkEventsTeeHandler(writer http.ResponseWriter, request *http.Request) {
	once.Do(func() {
		observers = model.NewObservers()
	})

	updateChan, errChan := OpenStream(
		writer,
		request.Context().Done(),
		Message{Event: TYPE_PUT, Data: []byte{}},
	)
	defer close(updateChan)

	observerId := observers.RegisterObserver(sdkEventObserver{updateChan})
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
