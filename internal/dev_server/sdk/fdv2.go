package sdk

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

const (
	fdv2ReasonUpToDate       = "up-to-date"
	fdv2ReasonCantCatchup    = "cant-catchup"
	fdv2ReasonPayloadMissing = "payload-missing"
	fdv2ReasonUpdate         = "update"
)

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
//
// Delta transfers are not supported: stale clients always receive a full payload.
// Tracking the change history required for deltas is overkill for a local dev server.
func buildInitialResponse(payloadID string, currentVersion int, flags model.FlagsState, basis string) (subsystems.PollingPayload, error) {
	basisPayloadID, basisVersion := parseBasis(basis)
	switch {
	case basisVersion == 0:
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonPayloadMissing)
	case basisPayloadID == payloadID && basisVersion == currentVersion:
		event, err := makeServerIntentEvent(payloadID, currentVersion, subsystems.IntentNone, fdv2ReasonUpToDate)
		if err != nil {
			return subsystems.PollingPayload{}, err
		}
		return subsystems.PollingPayload{Events: []subsystems.RawEvent{event}}, nil
	default:
		// Payload ID mismatch, stale version, or version ahead of current (e.g. project recreated):
		// we can't compute a delta — send the full payload.
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonCantCatchup)
	}
}

func buildFullTransferResponse(payloadID string, version int, flags model.FlagsState, reason string) (subsystems.PollingPayload, error) {
	intentEvent, err := makeServerIntentEvent(payloadID, version, subsystems.IntentTransferFull, reason)
	if err != nil {
		return subsystems.PollingPayload{}, err
	}
	events := []subsystems.RawEvent{intentEvent}

	for key, flagState := range flags {
		event, err := makePutObjectEvent(version, key, flagState)
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

func makePutObjectEvent(version int, key string, flagState model.FlagState) (subsystems.RawEvent, error) {
	object, err := json.Marshal(serverFlagFromFlagState(key, flagState))
	if err != nil {
		return subsystems.RawEvent{}, err
	}
	data, err := json.Marshal(subsystems.PutObject{
		Version: version,
		Kind:    subsystems.FlagKind,
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
func buildFlagChangeEvents(payloadID string, version int, flagKey string, flagState model.FlagState) ([]subsystems.RawEvent, error) {
	intentEvent, err := makeServerIntentEvent(payloadID, version, subsystems.IntentTransferChanges, fdv2ReasonUpdate)
	if err != nil {
		return nil, err
	}
	putEvent, err := makePutObjectEvent(version, flagKey, flagState)
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
