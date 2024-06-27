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
	store := model.StoreFromContext(ctx)
	projectKey := GetProjectKeyFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		panic(errors.Wrap(err, "unable to get dev project"))
	}
	allFlags, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		panic(errors.Wrap(err, "failed to get flag state"))
	}
	jsonBody, err := json.Marshal(allFlags)
	if err != nil {
		panic(errors.Wrap(err, "failed to marshal flag state"))
	}
	updateChan, doneChan := OpenStream(w, r.Context().Done(), Message{"put", jsonBody}) // TODO Wireup updateChan
	defer close(updateChan)
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
		panic(errors.Wrap(err, "stream failure"))
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
		data, err := json.Marshal(clientSidePatchData{
			Key:     event.FlagKey,
			Version: event.FlagState.Version,
			Value:   event.FlagState.Value,
		})
		if err != nil {
			panic(errors.Wrap(err, "failed to marshal flag state in observer"))
		}
		c.updateChan <- Message{
			Event: "patch",
			Data:  data,
		}
	}
}

type clientSidePatchData struct {
	Key     string        `json:"key"`
	Version int           `json:"version"`
	Value   ldvalue.Value `json:"value"`
}
