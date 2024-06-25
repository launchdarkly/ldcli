package model

import (
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
)

type Override struct {
	ProjectKey string
	FlagKey    string
	Value      ldvalue.Value
	Active     bool
	Version    int
}

func (o Override) Apply(state flagstate.FlagState) flagstate.FlagState {
	flagVersion := state.Version + o.Version
	flagValue := state.Value
	if o.Active {
		flagValue = o.Value
	}
	return flagstate.FlagState{
		Value:                flagValue,
		Variation:            ldvalue.NewOptionalIntFromPointer(nil),
		Version:              flagVersion,
		Reason:               ldreason.NewEvalReasonFallthrough(),
		TrackEvents:          false,
		TrackReason:          false,
		DebugEventsUntilDate: 0,
		OmitDetails:          true,
	}
}

type Overrides []Override

func (o Overrides) GetFlag(key string) (Override, bool) {
	for _, override := range o {
		if override.FlagKey == key {
			return override, true
		}
	}
	return Override{}, false
}
