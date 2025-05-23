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

// getFlagStateForFlagAndProject fetches state from the store so that it can later be used to apply an override and
// construct an update. You want to call this before you write the override so that written overrides don't
// less often don't cause updates.
func getFlagStateForFlagAndProject(ctx context.Context, projectKey, flagKey string) (FlagState, error) {
	store := StoreFromContext(ctx)

	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		return FlagState{}, err
	}

	var flagExists bool
	for flag := range project.AllFlagsState {
		if flagKey == flag {
			flagExists = true
			break
		}
	}
	if !flagExists {
		return FlagState{}, NewErrNotFound("flag", flagKey)
	}
	return project.AllFlagsState[flagKey], nil
}

func UpsertOverride(ctx context.Context, projectKey, flagKey string, value ldvalue.Value) (Override, error) {
	flagState, err := getFlagStateForFlagAndProject(ctx, projectKey, flagKey)
	if err != nil {
		return Override{}, err
	}

	override := Override{
		ProjectKey: projectKey,
		FlagKey:    flagKey,
		Value:      value,
		Active:     true,
		Version:    1,
	}

	store := StoreFromContext(ctx)
	override, err = store.UpsertOverride(ctx, override)
	if err != nil {
		return Override{}, err
	}

	GetObserversFromContext(ctx).Notify(OverrideEvent{
		FlagKey:    flagKey,
		ProjectKey: projectKey,
		FlagState:  override.Apply(flagState),
	})
	return override, nil
}

func DeleteOverride(ctx context.Context, projectKey, flagKey string) error {
	flagState, err := getFlagStateForFlagAndProject(ctx, projectKey, flagKey)
	if err != nil {
		return err
	}
	store := StoreFromContext(ctx)
	version, err := store.DeactivateOverride(ctx, projectKey, flagKey)
	if err != nil {
		return err
	}
	override := Override{
		ProjectKey: projectKey,
		FlagKey:    flagKey,
		Value:      ldvalue.Null(), // since inactive, will get use the one from flagState
		Active:     false,
		Version:    version,
	}
	GetObserversFromContext(ctx).Notify(OverrideEvent{
		FlagKey:    flagKey,
		ProjectKey: projectKey,
		FlagState:  override.Apply(flagState),
	})
	return err
}

func DeleteOverrides(ctx context.Context, projectKey string) error {

	store := StoreFromContext(ctx)
	overrides, err := store.GetOverridesForProject(ctx, projectKey)
	if err != nil {
		return err
	}

	for _, override := range overrides {
		err := DeleteOverride(ctx, projectKey, override.FlagKey)
		if err != nil {
			return err
		}
	}

	return nil
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
