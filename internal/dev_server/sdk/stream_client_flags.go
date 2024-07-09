package sdk

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func StreamClientFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allFlags, err := GetAllFlagsFromContext(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}
	jsonBody, err := json.Marshal(allFlags)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to marshal flag state"))
		return
	}
	updateChan, doneChan := OpenStream(
		w,
		r.Context().Done(),
		Message{Event: TYPE_PUT, Data: jsonBody},
	)
	defer close(updateChan)
	projectKey := GetProjectKeyFromContext(ctx)
	observer := clientFlagsObserver{updateChan, projectKey}
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

type clientFlagsObserver struct {
	updateChan chan<- Message
	projectKey string
}

func (c clientFlagsObserver) Handle(event interface{}) {
	log.Printf("clientFlagsObserver: handling flag state event: %v", event)
	switch event := event.(type) {
	case model.UpsertOverrideEvent:
		err := SendMessage(c.updateChan, TYPE_PATCH, clientFlag{
			Key:     event.FlagKey,
			Version: event.FlagState.Version,
			Value:   event.FlagState.Value,
		})
		if err != nil {
			panic(errors.Wrap(err, "failed to marshal flag state in observer"))
		}
	case model.SyncEvent:
		clientFlags := clientFlags{}
		for flagKey, flagState := range event.AllFlagsState {
			clientFlags[flagKey] = clientFlag{
				Version: flagState.Version,
				Value:   flagState.Value,
			}
		}

		err := SendMessage(c.updateChan, TYPE_PUT, clientFlags)
		if err != nil {
			panic(errors.Wrap(err, "failed to marshal flag state in observer"))
		}
	}
}

type clientFlag struct {
	Key     string        `json:"key,omitempty"`
	Version int           `json:"version"`
	Value   ldvalue.Value `json:"value"`
}

type clientFlags map[string]clientFlag
