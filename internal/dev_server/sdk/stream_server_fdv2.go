package sdk

import (
	"fmt"
	"log"
	"net/http"
	"time"

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

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(ctx, w, errors.New("streaming not supported"))
		return
	}

	initialPayload, err := buildFullTransferResponse(projectKey, project.PayloadVersion, allFlags, fdv2ReasonPayloadMissing)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to build initial payload"))
		return
	}

	// Register observer before writing to the client so that any changes arriving
	// during the initial write are queued and delivered immediately after.
	updateChan := make(chan []subsystems.RawEvent, 10)
	observerID := model.GetObserversFromContext(ctx).RegisterObserver(fdv2StreamObserver{
		updateChan: updateChan,
		projectKey: projectKey,
	})
	defer func() {
		if ok := model.GetObserversFromContext(ctx).DeregisterObserver(observerID); !ok {
			log.Printf("unable to deregister fdv2 stream observer")
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	if err := writeFDv2SSEEvents(w, flusher, initialPayload.Events); err != nil {
		return
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case events := <-updateChan:
			if err := writeFDv2SSEEvents(w, flusher, events); err != nil {
				return
			}
		case <-ticker.C:
			// SSE comment line as a keepalive.
			if _, err := w.Write([]byte(":\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// writeFDv2SSEEvents writes a batch of FDv2 events to the response as individual SSE events.
func writeFDv2SSEEvents(w http.ResponseWriter, flusher http.Flusher, events []subsystems.RawEvent) error {
	for _, event := range events {
		if _, err := fmt.Fprintf(w, "event:%s\ndata:%s\n\n", event.Name, event.Data); err != nil {
			return err
		}
	}
	flusher.Flush()
	return nil
}

type fdv2StreamObserver struct {
	updateChan chan<- []subsystems.RawEvent
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
		o.updateChan <- events
	case model.SyncEvent:
		if event.ProjectKey != o.projectKey {
			return
		}
		payload, err := buildFullTransferResponse(o.projectKey, event.PayloadVersion, event.AllFlagsState, fdv2ReasonCantCatchup)
		if err != nil {
			panic(errors.Wrap(err, "failed to build full transfer in fdv2 stream observer"))
		}
		o.updateChan <- payload.Events
	}
}
