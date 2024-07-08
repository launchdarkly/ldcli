package sdk

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func StreamServerAllPayload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectKey := GetProjectKeyFromContext(ctx)
	allFlags, err := GetAllFlagsFromContext(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}
	serverFlags := ServerAllPayloadFromFlagsState(allFlags)
	jsonBody, err := json.Marshal(serverFlags)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to marshal flag state"))
		return
	}
	updateChan, doneChan := OpenStream(w, r.Context().Done(), Message{"put", jsonBody})
	defer close(updateChan)
	observer := serverFlagsObserver{updateChan, projectKey}
	observers := model.GetObserversFromContext(ctx)
	observerId := observers.RegisterObserver(observer)
	defer func() {
		ok := observers.DeregisterObserver(observerId)
		if !ok {
			log.Printf("unable to remove observer")
		}
	}()
	err = <-doneChan
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "stream failure"))
		return
	}
}

type serverFlagsObserver struct {
	updateChan chan<- Message
	projectKey string
}

func (c serverFlagsObserver) Handle(event interface{}) {
	log.Printf("clientFlagsObserver: handling flag state event: %v", event)
	switch event := event.(type) {
	case model.UpsertOverrideEvent:
		if event.ProjectKey != c.projectKey {
			return
		}

		data, err := json.Marshal(serverSidePatchData{
			Path: fmt.Sprintf("/flags/%s", event.FlagKey),
			Data: serverFlagFromFlagState(event.FlagKey, event.FlagState),
		})
		if err != nil {
			panic(errors.Wrap(err, "failed to marshal flag state in observer"))
		}

		c.updateChan <- Message{
			Event: "patch",
			Data:  data,
		}
	case model.SyncEvent:
		if event.ProjectKey != c.projectKey {
			return
		}

		data, err := json.Marshal(ServerAllPayloadFromFlagsState(event.AllFlagsState))
		if err != nil {
			panic(errors.Wrap(err, "failed to marshal flag state in observer"))
		}

		c.updateChan <- Message{
			Event: "put",
			Data:  data,
		}
	}
}

type serverSidePatchData struct {
	Path string     `json:"path"`
	Data ServerFlag `json:"data"`
}
