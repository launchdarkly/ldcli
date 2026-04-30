package sdk

import (
	"fmt"
	"log"
	"net/http"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func StreamV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectKey := GetProjectKeyFromContext(ctx)
	store := model.StoreFromContext(ctx)

	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get project"))
		return
	}

	allFlags, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}

	initialPayload, err := buildInitialResponse(projectKey, project.PayloadVersion, allFlags, r.URL.Query().Get("basis"))
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to build initial payload"))
		return
	}

	updateChan, doneChan := OpenStream(w, r.Context().Done(), fdv2SSEPayload(initialPayload.Events))
	defer close(updateChan)

	observer := fdv2StreamObserver{updateChan: updateChan, projectKey: projectKey}
	observerID := model.GetObserversFromContext(ctx).RegisterObserver(observer)
	defer func() {
		if ok := model.GetObserversFromContext(ctx).DeregisterObserver(observerID); !ok {
			log.Printf("unable to deregister fdv2 stream observer")
		}
	}()

	err = <-doneChan
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "stream failure"))
	}
}

// fdv2SSEPayload formats a slice of FDv2 events as raw SSE bytes.
// Each event becomes an individual SSE event in the output.
func fdv2SSEPayload(events []subsystems.RawEvent) []byte {
	var buf []byte
	for _, e := range events {
		buf = append(buf, fmt.Sprintf("event:%s\ndata:%s\n\n", e.Name, e.Data)...)
	}
	return buf
}

type fdv2StreamObserver struct {
	updateChan chan<- []byte
	projectKey string
}

func (o fdv2StreamObserver) Handle(event interface{}) {
	switch event := event.(type) {
	case model.OverrideEvent:
		if event.ProjectKey != o.projectKey {
			return
		}
		events, err := buildFlagChangeEvents(o.projectKey, event.PayloadVersion, event.FlagKey, event.FlagState)
		if err != nil {
			panic(errors.Wrap(err, "failed to build flag change events in fdv2 stream observer"))
		}
		o.updateChan <- fdv2SSEPayload(events)
	case model.SyncEvent:
		if event.ProjectKey != o.projectKey {
			return
		}
		payload, err := buildFullTransferResponse(o.projectKey, event.PayloadVersion, event.AllFlagsState, fdv2ReasonCantCatchup)
		if err != nil {
			panic(errors.Wrap(err, "failed to build full transfer in fdv2 stream observer"))
		}
		o.updateChan <- fdv2SSEPayload(payload.Events)
	}
}
