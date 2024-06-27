package model

import (
	"context"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"github.com/pkg/errors"
)

type Project struct {
	Key                  string
	SourceEnvironmentKey string
	Context              ldcontext.Context
	LastSyncTime         time.Time
	FlagState            FlagsState
}

// CreateProject creates a project and adds it to the database.
func CreateProject(ctx context.Context, projectKey, sourceEnvironmentKey string, ldCtx *ldcontext.Context) (Project, error) {
	store := StoreFromContext(ctx)
	project := Project{}
	project.Key = projectKey
	project.SourceEnvironmentKey = sourceEnvironmentKey
	if ldCtx == nil {
		project.Context = ldcontext.NewBuilder("user").Key("dev-environment").Build()
	} else {
		project.Context = *ldCtx
	}
	flagsState, err := project.FetchFlagState(ctx)
	if err != nil {
		return Project{}, err
	}

	project.FlagState = flagsState
	project.LastSyncTime = time.Now()

	err = store.InsertProject(ctx, project)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func UpdateProject(ctx context.Context, projectKey string, context *ldcontext.Context, sourceEnvironmentKey *string) (Project, error) {
	store := StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		return Project{}, err
	}
	if context != nil {
		project.Context = *context
	}

	if sourceEnvironmentKey != nil {
		project.SourceEnvironmentKey = *sourceEnvironmentKey
	}

	if context != nil || sourceEnvironmentKey != nil {
		flagsState, err := project.FetchFlagState(ctx)
		if err != nil {
			return Project{}, err
		}
		project.FlagState = flagsState
		project.LastSyncTime = time.Now()
	}

	updated, err := store.UpdateProject(ctx, *project)
	if err != nil {
		return Project{}, err
	}
	if !updated {
		return Project{}, err
	}
	return *project, nil
}

func SyncProject(ctx context.Context, projectKey string) (Project, error) {
	store := StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		return Project{}, err
	}
	flagsState, err := project.FetchFlagState(ctx)
	if err != nil {
		return Project{}, err
	}

	project.FlagState = flagsState
	project.LastSyncTime = time.Now()
	updated, err := store.UpdateProject(ctx, *project)
	if err != nil {
		return Project{}, err
	}
	if !updated {
		return Project{}, err
	}
	return *project, nil
}

func (p Project) GetFlagStateWithOverridesForProject(ctx context.Context) (FlagsState, error) {
	store := StoreFromContext(ctx)
	overrides, err := store.GetOverridesForProject(ctx, p.Key)
	if err != nil {
		return FlagsState{}, errors.Wrapf(err, "unable to fetch overrides for project %s", p.Key)
	}
	withOverrides := make(FlagsState, len(p.FlagState))
	for flagKey, flagState := range p.FlagState {
		if override, ok := overrides.GetFlag(flagKey); ok {
			flagState = override.Apply(flagState)
		}
		withOverrides[flagKey] = flagState
	}
	return withOverrides, nil
}

func (p Project) FetchFlagState(ctx context.Context) (FlagsState, error) {
	sdkAdapter := adapters.GetSdk(ctx)
	apiAdapter := adapters.GetApi(ctx)
	sdkKey, err := apiAdapter.GetSdkKey(ctx, p.Key, p.SourceEnvironmentKey)
	flagsState := make(FlagsState)
	if err != nil {
		return flagsState, err
	}
	sdkFlags, err := sdkAdapter.GetAllFlagsState(ctx, p.Context, sdkKey)
	if err != nil {
		return flagsState, err
	}
	flagsState = FromAllFlags(sdkFlags)
	return flagsState, nil
}
