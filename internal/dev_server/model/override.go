package model

import (
	"context"
	"encoding/json"

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

func UpsertOverride(ctx context.Context, projectKey, flagKey, value string) (Override, error) {
	var val ldvalue.Value
	err := json.Unmarshal([]byte(value), &val)
	if err != nil {
		return Override{}, err
	}

	override := Override{
		ProjectKey: projectKey,
		FlagKey:    flagKey,
		Value:      val,
		Active:     true,
		Version:    1,
	}
	store := StoreFromContext(ctx)
	err = store.UpsertOverride(ctx, override)
	if err != nil {
		return Override{}, err
	}

	return override, err
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
