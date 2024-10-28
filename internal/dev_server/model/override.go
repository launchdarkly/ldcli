package model

import (
	"context"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
)

type Override struct {
	ProjectKey string
	FlagKey    string
	Value      ldvalue.Value
	Active     bool
	Version    int
}

func UpsertOverride(ctx context.Context, projectKey, flagKey string, value ldvalue.Value) (Override, error) {
	// TODO: validate if the flag type matches

	store := StoreFromContext(ctx)

	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil || project == nil {
		return Override{}, NewError("project does not exist within dev server")
	}

	var flagExists bool
	for flag := range project.AllFlagsState {
		if flagKey == flag {
			flagExists = true
			break
		}
	}
	if !flagExists {
		return Override{}, NewError("flag does not exist within dev project")
	}

	override := Override{
		ProjectKey: projectKey,
		FlagKey:    flagKey,
		Value:      value,
		Active:     true,
		Version:    1,
	}

	override, err = store.UpsertOverride(ctx, override)
	if err != nil {
		return Override{}, err
	}

	flagState := override.Apply(project.AllFlagsState[flagKey])
	GetObserversFromContext(ctx).Notify(UpsertOverrideEvent{
		FlagKey:    flagKey,
		ProjectKey: projectKey,
		FlagState:  flagState,
	})

	return override, nil
}

func DeleteOverride(ctx context.Context, projectKey, flagKey string) error {
	store := StoreFromContext(ctx)
	err := store.DeactivateOverride(ctx, projectKey, flagKey)
	return err
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
