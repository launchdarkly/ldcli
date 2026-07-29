package sdk

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

const (
	fdv2ReasonUpToDate       = "up-to-date"
	fdv2ReasonCantCatchup    = "cant-catchup"
	fdv2ReasonPayloadMissing = "payload-missing"
	fdv2ReasonUpdate         = "update"
)

// fdv2ObjectEncoder describes how a flag is represented inside a put-object event.
// The server-side and client-side FDv2 protocols share every other event type, and
// differ only here: server-side SDKs receive the flag configuration and evaluate it
// themselves, while client-side SDKs receive a result the server already evaluated.
type fdv2ObjectEncoder struct {
	kind   subsystems.ObjectKind
	encode func(key string, flagState model.FlagState) any
}

// fdv2ServerObjects encodes flags for server-side SDKs as full flag configurations.
var fdv2ServerObjects = fdv2ObjectEncoder{
	kind: subsystems.FlagKind,
	encode: func(key string, flagState model.FlagState) any {
		return serverFlagFromFlagState(key, flagState)
	},
}

// parseBasis extracts the payload ID and version from a basis state string of the
// form "(p:<payloadId>:<version>)". Returns ("", 0) if the string is absent or unparseable.
//
// Note: in production LD selectors the payload ID is an opaque server-assigned value.
// The dev server uses the project key as the payload ID (see makePayloadTransferredEvent).
// This is a dev-server-specific convention and should not be assumed elsewhere.
func parseBasis(basis string) (string, int) {
	if !strings.HasPrefix(basis, "(p:") || !strings.HasSuffix(basis, ")") {
		return "", 0
	}
	// Strip the "(p:" prefix and ")" suffix to get "<payloadId>:<version>".
	inner := basis[3 : len(basis)-1]
	lastColon := strings.LastIndex(inner, ":")
	if lastColon == -1 {
		return "", 0
	}
	version, err := strconv.Atoi(inner[lastColon+1:])
	if err != nil || version < 0 {
		return "", 0
	}
	return inner[:lastColon], version
}

// buildInitialResponse constructs the FDv2 initial response for both polling and streaming.
//
// payloadID is the stable identifier for this payload (the project key).
// currentVersion is the project's current PayloadVersion.
// flags is the current flag state with overrides applied.
// basis is the raw ?basis query param from the SDK (empty string = no basis provided).
// objects controls how each flag is encoded into its put-object event.
//
// Delta transfers are not supported: stale clients always receive a full payload.
// Tracking the change history required for deltas is overkill for a local dev server.
func buildInitialResponse(payloadID string, currentVersion int, flags model.FlagsState, basis string, objects fdv2ObjectEncoder) (subsystems.PollingPayload, error) {
	basisPayloadID, basisVersion := parseBasis(basis)
	switch {
	case basisVersion == 0:
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonPayloadMissing, objects)
	case basisPayloadID == payloadID && basisVersion == currentVersion:
		event, err := makeServerIntentEvent(payloadID, currentVersion, subsystems.IntentNone, fdv2ReasonUpToDate)
		if err != nil {
			return subsystems.PollingPayload{}, err
		}
		return subsystems.PollingPayload{Events: []subsystems.RawEvent{event}}, nil
	default:
		// Payload ID mismatch, stale version, or version ahead of current (e.g. project recreated):
		// we can't compute a delta — send the full payload.
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonCantCatchup, objects)
	}
}

func buildFullTransferResponse(payloadID string, version int, flags model.FlagsState, reason string, objects fdv2ObjectEncoder) (subsystems.PollingPayload, error) {
	intentEvent, err := makeServerIntentEvent(payloadID, version, subsystems.IntentTransferFull, reason)
	if err != nil {
		return subsystems.PollingPayload{}, err
	}
	events := []subsystems.RawEvent{intentEvent}

	for key, flagState := range flags {
		event, err := makePutObjectEvent(version, key, flagState, objects)
		if err != nil {
			return subsystems.PollingPayload{}, err
		}
		events = append(events, event)
	}

	transferredEvent, err := makePayloadTransferredEvent(payloadID, version)
	if err != nil {
		return subsystems.PollingPayload{}, err
	}
	events = append(events, transferredEvent)

	return subsystems.PollingPayload{Events: events}, nil
}

func makeServerIntentEvent(payloadID string, target int, intentCode subsystems.IntentCode, reason string) (subsystems.RawEvent, error) {
	data, err := json.Marshal(subsystems.ServerIntent{
		Payload: subsystems.Payload{
			ID:     payloadID,
			Target: target,
			Code:   intentCode,
			Reason: reason,
		},
	})
	if err != nil {
		return subsystems.RawEvent{}, err
	}
	return subsystems.RawEvent{Name: subsystems.EventServerIntent, Data: data}, nil
}

func makePutObjectEvent(version int, key string, flagState model.FlagState, objects fdv2ObjectEncoder) (subsystems.RawEvent, error) {
	object, err := json.Marshal(objects.encode(key, flagState))
	if err != nil {
		return subsystems.RawEvent{}, err
	}
	data, err := json.Marshal(subsystems.PutObject{
		Version: version,
		Kind:    objects.kind,
		Key:     key,
		Object:  object,
	})
	if err != nil {
		return subsystems.RawEvent{}, err
	}
	return subsystems.RawEvent{Name: subsystems.EventPutObject, Data: data}, nil
}

// buildFlagChangeEvents builds the events sequence for a single flag update pushed over a stream:
// server-intent(xfer-changes) + put-object(changed flag) + payload-transferred.
func buildFlagChangeEvents(payloadID string, version int, flagKey string, flagState model.FlagState, objects fdv2ObjectEncoder) ([]subsystems.RawEvent, error) {
	intentEvent, err := makeServerIntentEvent(payloadID, version, subsystems.IntentTransferChanges, fdv2ReasonUpdate)
	if err != nil {
		return nil, err
	}
	putEvent, err := makePutObjectEvent(version, flagKey, flagState, objects)
	if err != nil {
		return nil, err
	}
	transferredEvent, err := makePayloadTransferredEvent(payloadID, version)
	if err != nil {
		return nil, err
	}
	return []subsystems.RawEvent{intentEvent, putEvent, transferredEvent}, nil
}

func makePayloadTransferredEvent(payloadID string, version int) (subsystems.RawEvent, error) {
	// The selector state is synthetic and dev-server-specific: the dev server uses the
	// project key as the payload ID rather than a server-assigned opaque value. The SDK
	// echoes this selector back as ?basis on subsequent polls, where parseBasis
	// extracts the payload ID and version from it.
	selector := subsystems.NewSelector(fmt.Sprintf("(p:%s:%d)", payloadID, version), version)
	data, err := json.Marshal(selector)
	if err != nil {
		return subsystems.RawEvent{}, err
	}
	return subsystems.RawEvent{Name: subsystems.EventPayloadTransferred, Data: data}, nil
}

// buildRequestInitialResponse loads the current flag state for the project on the
// request context and turns it into the initial FDv2 response, honouring the ?basis the
// SDK echoed back from its last payload-transferred event.
func buildRequestInitialResponse(r *http.Request, objects fdv2ObjectEncoder) (subsystems.PollingPayload, error) {
	ctx := r.Context()
	projectKey := GetProjectKeyFromContext(ctx)

	project, err := model.StoreFromContext(ctx).GetDevProject(ctx, projectKey)
	if err != nil {
		return subsystems.PollingPayload{}, errors.Wrap(err, "failed to get project")
	}

	allFlags, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		return subsystems.PollingPayload{}, errors.Wrap(err, "failed to get flag state")
	}

	response, err := buildInitialResponse(projectKey, project.PayloadVersion, allFlags, r.URL.Query().Get("basis"), objects)
	return response, errors.Wrap(err, "failed to build initial payload")
}

// serveFdv2Poll answers an FDv2 polling request for the project on the request context.
// Server-side and client-side polling differ only in how flags are encoded.
func serveFdv2Poll(w http.ResponseWriter, r *http.Request, objects fdv2ObjectEncoder) {
	ctx := r.Context()

	response, err := buildRequestInitialResponse(r, objects)
	if err != nil {
		WriteError(ctx, w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to encode response"))
	}
}

// serveFdv2Stream answers an FDv2 streaming request for the project on the request
// context: the same initial response a poll would return, followed by an SSE stream of
// updates until the client disconnects. Server-side and client-side streaming differ
// only in how flags are encoded.
func serveFdv2Stream(w http.ResponseWriter, r *http.Request, objects fdv2ObjectEncoder) {
	ctx := r.Context()
	projectKey := GetProjectKeyFromContext(ctx)

	initialPayload, err := buildRequestInitialResponse(r, objects)
	if err != nil {
		WriteError(ctx, w, err)
		return
	}

	updateChan, doneChan := OpenStream(w, ctx.Done(), fdv2SSEPayload(initialPayload.Events))
	defer close(updateChan)

	observers := model.GetObserversFromContext(ctx)
	observerID := observers.RegisterObserver(fdv2StreamObserver{
		updateChan: updateChan,
		projectKey: projectKey,
		objects:    objects,
	})
	defer func() {
		if ok := observers.DeregisterObserver(observerID); !ok {
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
	objects    fdv2ObjectEncoder
}

func (o fdv2StreamObserver) Handle(event interface{}) {
	switch event := event.(type) {
	case model.OverrideEvent:
		if event.ProjectKey != o.projectKey {
			return
		}
		events, err := buildFlagChangeEvents(o.projectKey, event.PayloadVersion, event.FlagKey, event.FlagState, o.objects)
		if err != nil {
			panic(errors.Wrap(err, "failed to build flag change events in fdv2 stream observer"))
		}
		o.updateChan <- fdv2SSEPayload(events)
	case model.SyncEvent:
		if event.ProjectKey != o.projectKey {
			return
		}
		payload, err := buildFullTransferResponse(o.projectKey, event.PayloadVersion, event.AllFlagsState, fdv2ReasonCantCatchup, o.objects)
		if err != nil {
			panic(errors.Wrap(err, "failed to build full transfer in fdv2 stream observer"))
		}
		o.updateChan <- fdv2SSEPayload(payload.Events)
	}
}
