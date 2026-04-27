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
)

// parseBasisVersion extracts the payload version from a basis state string of the
// form "(p:<payloadId>:<version>)". Returns 0 if the string is absent or unparseable.
func parseBasisVersion(basis string) int {
	if basis == "" {
		return 0
	}
	lastColon := strings.LastIndex(basis, ":")
	if lastColon == -1 {
		return 0
	}
	versionStr := strings.TrimSuffix(basis[lastColon+1:], ")")
	version, err := strconv.Atoi(versionStr)
	if err != nil || version < 0 {
		return 0
	}
	return version
}

// buildPollResponse constructs the FDv2 polling response.
//
// payloadID is the stable identifier for this payload (the project key).
// currentVersion is the project's current PayloadVersion.
// flags is the current flag state with overrides applied.
// basisVersion is parsed from the SDK's ?basis query param (0 = no basis provided).
//
// Delta transfers are not supported: stale clients always receive a full payload.
// Tracking the change history required for deltas is overkill for a local dev server.
func buildPollResponse(payloadID string, currentVersion int, flags model.FlagsState, basisVersion int) (subsystems.PollingPayload, error) {
	switch {
	case basisVersion == 0:
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonPayloadMissing)
	case basisVersion >= currentVersion:
		event, err := makeServerIntentEvent(payloadID, currentVersion, subsystems.IntentNone, fdv2ReasonUpToDate)
		if err != nil {
			return subsystems.PollingPayload{}, err
		}
		return subsystems.PollingPayload{Events: []subsystems.RawEvent{event}}, nil
	default:
		// Stale: we don't store history so we can't compute a delta — send the full payload.
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

func makePayloadTransferredEvent(payloadID string, version int) (subsystems.RawEvent, error) {
	selector := subsystems.NewSelector(fmt.Sprintf("(p:%s:%d)", payloadID, version), version)
	data, err := json.Marshal(selector)
	if err != nil {
		return subsystems.RawEvent{}, err
	}
	return subsystems.RawEvent{Name: subsystems.EventPayloadTransferred, Data: data}, nil
}
