package sdk

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

const (
	fdv2EventServerIntent       = "server-intent"
	fdv2EventPutObject          = "put-object"
	fdv2EventPayloadTransferred = "payload-transferred"

	fdv2IntentXferFull = "xfer-full"
	fdv2IntentNone     = "none"

	fdv2ReasonUpToDate       = "up-to-date"
	fdv2ReasonCantCatchup    = "cant-catchup"
	fdv2ReasonPayloadMissing = "payload-missing"
)

// fdv2RawEvent matches the wire format the SDK deserializes from the /sdk/poll response.
// The SDK's RawEvent uses json:"event" (not json:"name") as of v7.13+.
type fdv2RawEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type fdv2Payload struct {
	ID         string `json:"id"`
	Target     int    `json:"target"`
	IntentCode string `json:"intentCode"`
	Reason     string `json:"reason"`
}

type fdv2ServerIntentData struct {
	Payloads []fdv2Payload `json:"payloads"`
}

type fdv2PutObjectData struct {
	Version int             `json:"version"`
	Kind    string          `json:"kind"`
	Key     string          `json:"key"`
	Object  json.RawMessage `json:"object"`
}

type fdv2PayloadTransferredData struct {
	State   string `json:"state"`
	Version int    `json:"version"`
}

type fdv2PollResponse struct {
	Events []fdv2RawEvent `json:"events"`
}

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
func buildPollResponse(payloadID string, currentVersion int, flags model.FlagsState, basisVersion int) (fdv2PollResponse, error) {
	switch {
	case basisVersion == 0:
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonPayloadMissing)
	case basisVersion >= currentVersion:
		event, err := makeServerIntentEvent(payloadID, currentVersion, fdv2IntentNone, fdv2ReasonUpToDate)
		if err != nil {
			return fdv2PollResponse{}, err
		}
		return fdv2PollResponse{Events: []fdv2RawEvent{event}}, nil
	default:
		// Stale: we don't store history so we can't compute a delta — send the full payload.
		return buildFullTransferResponse(payloadID, currentVersion, flags, fdv2ReasonCantCatchup)
	}
}

func buildFullTransferResponse(payloadID string, version int, flags model.FlagsState, reason string) (fdv2PollResponse, error) {
	intentEvent, err := makeServerIntentEvent(payloadID, version, fdv2IntentXferFull, reason)
	if err != nil {
		return fdv2PollResponse{}, err
	}
	events := []fdv2RawEvent{intentEvent}

	for key, flagState := range flags {
		event, err := makePutObjectEvent(version, key, flagState)
		if err != nil {
			return fdv2PollResponse{}, err
		}
		events = append(events, event)
	}

	transferredEvent, err := makePayloadTransferredEvent(payloadID, version)
	if err != nil {
		return fdv2PollResponse{}, err
	}
	events = append(events, transferredEvent)

	return fdv2PollResponse{Events: events}, nil
}

func makeServerIntentEvent(payloadID string, target int, intentCode, reason string) (fdv2RawEvent, error) {
	data, err := json.Marshal(fdv2ServerIntentData{
		Payloads: []fdv2Payload{{
			ID:         payloadID,
			Target:     target,
			IntentCode: intentCode,
			Reason:     reason,
		}},
	})
	if err != nil {
		return fdv2RawEvent{}, err
	}
	return fdv2RawEvent{Event: fdv2EventServerIntent, Data: data}, nil
}

func makePutObjectEvent(version int, key string, flagState model.FlagState) (fdv2RawEvent, error) {
	object, err := json.Marshal(serverFlagFromFlagState(key, flagState))
	if err != nil {
		return fdv2RawEvent{}, err
	}
	data, err := json.Marshal(fdv2PutObjectData{
		Version: version,
		Kind:    "flag",
		Key:     key,
		Object:  object,
	})
	if err != nil {
		return fdv2RawEvent{}, err
	}
	return fdv2RawEvent{Event: fdv2EventPutObject, Data: data}, nil
}

func makePayloadTransferredEvent(payloadID string, version int) (fdv2RawEvent, error) {
	data, err := json.Marshal(fdv2PayloadTransferredData{
		State:   fmt.Sprintf("(p:%s:%d)", payloadID, version),
		Version: version,
	})
	if err != nil {
		return fdv2RawEvent{}, err
	}
	return fdv2RawEvent{Event: fdv2EventPayloadTransferred, Data: data}, nil
}
