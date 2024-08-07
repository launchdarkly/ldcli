package model

import (
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
)

type FlagState struct {
	Value   ldvalue.Value `json:"value"`
	Version int           `json:"version"`
}

type FlagsState map[string]FlagState

func FromAllFlags(sdkFlags flagstate.AllFlags) FlagsState {
	flags := sdkFlags.ToValuesMap()
	flagsState := make(FlagsState, len(flags))
	for key, value := range flags {
		sdkFlag, ok := sdkFlags.GetFlag(key)
		if !ok {
			// panic because we're iterating over the same set of keys
			panic("flag '" + key + "' not found")
		}
		flagsState[key] = FlagState{
			Value:   value,
			Version: sdkFlag.Version,
		}
	}
	return flagsState
}
