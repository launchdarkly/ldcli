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
	project := Project{}
	project.Key = projectKey
	project.SourceEnvironmentKey = sourceEnvironmentKey
	if ldCtx == nil {
		project.Context = ldcontext.NewBuilder("user").Key("dev-environment").Build()
	} else {
		project.Context = *ldCtx
	}

	sdkAdapter := adapters.GetSdk(ctx)
	apiAdapter := adapters.GetApi(ctx)
	store := StoreFromContext(ctx)
	sdkKey, err := apiAdapter.GetSdkKey(ctx, projectKey, sourceEnvironmentKey)
	if err != nil {
		return Project{}, err
	}
	sdkFlags, err := sdkAdapter.GetAllFlagsState(ctx, project.Context, sdkKey)
	if err != nil {
		return Project{}, err
	}
	project.FlagState = FromAllFlags(sdkFlags)
	project.LastSyncTime = time.Now()

	err = store.InsertProject(ctx, project)
	if err != nil {
		return Project{}, err
	}
	return project, nil
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

func SyncProject(ctx context.Context, projectKey string) (Project, error) {
	store := StoreFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		return Project{}, err
	}
	sdkAdapter := adapters.GetSdk(ctx)
	apiAdapter := adapters.GetApi(ctx)
	sdkKey, err := apiAdapter.GetSdkKey(ctx, projectKey, project.SourceEnvironmentKey)
	if err != nil {
		return Project{}, err
	}
	sdkFlags, err := sdkAdapter.GetAllFlagsState(ctx, project.Context, sdkKey)
	if err != nil {
		return Project{}, err
	}
	project.FlagState = FromAllFlags(sdkFlags)
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