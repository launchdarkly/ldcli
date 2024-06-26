package model

import (
	"context"
	"encoding/json"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/pkg/errors"
)

type Override struct {
	ProjectKey string
	FlagKey    string
	Value      ldvalue.Value
	Active     bool
	Version    int
}

type UpsertOverrideEvent struct {
	FlagKey   string
	FlagState FlagState
}

func UpsertOverride(ctx context.Context, projectKey, flagKey, value string) (Override, error) {
	// TODO: validate flag exists within project, + if the flag type matches

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
	override, err = store.UpsertOverride(ctx, override)
	if err != nil {
		return Override{}, err
	}

	observers := GetObserversFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		return Override{}, errors.Wrap(err, "unable to get project")
	}
	flagState := override.Apply(project.FlagState[flagKey])
	observers.Notify(UpsertOverrideEvent{
		FlagKey:   flagKey,
		FlagState: flagState,
	})

	return override, err
}

func (o Override) Apply(state FlagState) FlagState {
	flagVersion := state.Version + o.Version
	flagValue := state.Value
	if o.Active {
		flagValue = o.Value
	}
	return FlagState{
		Value:   flagValue,
		Version: flagVersion,
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
