package sdk

import (
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

type fallthroughRule struct {
	Variation int `json:"variation"`
}

type clientSideAvailability struct {
	UsingMobileKey     bool `json:"usingMobileKey"`
	UsingEnvironmentId bool `json:"usingEnvironmentId"`
}

type ServerFlag struct {
	Key                    string                 `json:"key"`
	On                     bool                   `json:"on"`
	Prerequisites          []string               `json:"prerequisites"` // this isn't the real model for this, but this will always be empty for us
	Targets                []string               `json:"targets"`       // this isn't the real model for this, but this will always be empty for us
	Rules                  []string               `json:"rules"`         // this isn't the real model for this, but this will always be empty for us
	Fallthrough            fallthroughRule        `json:"fallthrough"`
	OffVariation           int                    `json:"offVariation"`
	Variations             []ldvalue.Value        `json:"variations"`
	ClientSideAvailability clientSideAvailability `json:"clientSideAvailability"`
	ClientSide             bool                   `json:"clientSide"`
	Salt                   string                 `json:"salt"`
	TrackEvents            bool                   `json:"trackEvents"`
	TrackEventsFallthrough bool                   `json:"trackEventsFallthrough"`
	DebugEventsUntilDate   int                    `json:"debugEventsUntilDate"`
	Version                int                    `json:"version"`
	Deleted                bool                   `json:"deleted"`
}

type ServerFlags map[string]ServerFlag

type data struct {
	Flags ServerFlags `json:"flags"`
}
type ServerAllPayload struct {
	Path string `json:"path"`
	Data data   `json:"data"`
}

func ServerAllPayloadFromFlagsState(state model.FlagsState) ServerAllPayload {
	return ServerAllPayload{
		Path: "",
		Data: data{ServerFlagsFromFlagsState(state)},
	}
}

func ServerFlagsFromFlagsState(flagsState model.FlagsState) ServerFlags {
	serverFlags := make(map[string]ServerFlag, len(flagsState))
	for flagKey, state := range flagsState {
		serverFlags[flagKey] = ServerFlagFromFlagState(flagKey, state)
	}
	return serverFlags
}

func ServerFlagFromFlagState(key string, state model.FlagState) ServerFlag {
	return ServerFlag{
		Key:                    key,
		On:                     true,
		Prerequisites:          make([]string, 0),
		Targets:                make([]string, 0),
		Rules:                  make([]string, 0),
		Fallthrough:            fallthroughRule{Variation: 0},
		OffVariation:           0,
		Variations:             []ldvalue.Value{state.Value},
		ClientSideAvailability: clientSideAvailability{true, true},
		ClientSide:             true,
		Salt:                   "",
		TrackEvents:            false,
		TrackEventsFallthrough: false,
		DebugEventsUntilDate:   0,
		Version:                state.Version,
		Deleted:                false,
	}
}
