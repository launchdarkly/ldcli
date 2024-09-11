package model

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
)

type Project struct {
	Key                  string
	SourceEnvironmentKey string
	Context              ldcontext.Context
	LastSyncTime         time.Time
	AllFlagsState        FlagsState
	AvailableVariations  []FlagVariation
}

// CreateProject creates a project and adds it to the database.
func CreateProject(ctx context.Context, projectKey, sourceEnvironmentKey string, ldCtx *ldcontext.Context) (Project, error) {
	project := Project{
		Key:                  projectKey,
		SourceEnvironmentKey: sourceEnvironmentKey,
	}

	if ldCtx == nil {
		project.Context = ldcontext.NewBuilder("user").Key("dev-environment").Build()
	} else {
		project.Context = *ldCtx
	}
	err := project.refreshExternalState(ctx)
	if err != nil {
		return Project{}, err
	}
	store := StoreFromContext(ctx)
	err = store.InsertProject(ctx, project)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (project *Project) refreshExternalState(ctx context.Context) error {
	flagsState, err := project.fetchFlagState(ctx)
	if err != nil {
		return err
	}
	project.AllFlagsState = flagsState
	project.LastSyncTime = time.Now()

	availableVariations, err := project.fetchAvailableVariations(ctx)
	if err != nil {
		return err
	}
	project.AvailableVariations = availableVariations
	return nil
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

	err = project.refreshExternalState(ctx)
	if err != nil {
		return Project{}, err
	}

	updated, err := store.UpdateProject(ctx, *project)
	if err != nil {
		return Project{}, err
	}
	if !updated {
		return Project{}, errors.New("Project not updated")
	}

	allFlagsWithOverrides, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		return Project{}, errors.Wrapf(err, "unable to get overrides for project, %s", projectKey)
	}

	GetObserversFromContext(ctx).Notify(SyncEvent{
		ProjectKey:    project.Key,
		AllFlagsState: allFlagsWithOverrides,
	})
	return *project, nil
}

func (project Project) GetFlagStateWithOverridesForProject(ctx context.Context) (FlagsState, error) {
	store := StoreFromContext(ctx)
	overrides, err := store.GetOverridesForProject(ctx, project.Key)
	if err != nil {
		return FlagsState{}, errors.Wrapf(err, "unable to fetch overrides for project %s", project.Key)
	}
	withOverrides := make(FlagsState, len(project.AllFlagsState))
	for flagKey, flagState := range project.AllFlagsState {
		if override, ok := overrides.GetFlag(flagKey); ok {
			flagState = override.Apply(flagState)
		}
		withOverrides[flagKey] = flagState
	}
	return withOverrides, nil
}

func (project Project) fetchAvailableVariations(ctx context.Context) ([]FlagVariation, error) {
	apiAdapter := adapters.GetApi(ctx)
	flags, err := apiAdapter.GetAllFlags(ctx, project.Key)
	if err != nil {
		return nil, err
	}
	var allVariations []FlagVariation
	for _, flag := range flags {
		flagKey := flag.Key
		for _, variation := range flag.Variations {
			allVariations = append(allVariations, FlagVariation{
				FlagKey: flagKey,
				Variation: Variation{
					Id:          *variation.Id,
					Description: variation.Description,
					Name:        variation.Name,
					Value:       ldvalue.CopyArbitraryValue(variation.Value),
				},
			})
		}
	}
	return allVariations, nil
}

func (project Project) Environments(ctx context.Context) ([]Environment, error) {
	apiAdapter := adapters.GetApi(ctx)
	environments, err := apiAdapter.GetProjectEnvironments(ctx, project.Key)
	if err != nil {
		return nil, err
	}

	var allEnvironments []Environment
	for _, environment := range environments {
		allEnvironments = append(allEnvironments, Environment{
			Key:  environment.Key,
			Name: environment.Name,
		})
	}

	return allEnvironments, nil
}

func (project Project) fetchFlagState(ctx context.Context) (FlagsState, error) {
	apiAdapter := adapters.GetApi(ctx)
	sdkKey, err := apiAdapter.GetSdkKey(ctx, project.Key, project.SourceEnvironmentKey)
	flagsState := make(FlagsState)
	if err != nil {
		return flagsState, err
	}

	sdkAdapter := adapters.GetSdk(ctx)
	sdkFlags, err := sdkAdapter.GetAllFlagsState(ctx, project.Context, sdkKey)
	if err != nil {
		return flagsState, err
	}

	flagsState = FromAllFlags(sdkFlags)
	return flagsState, nil
}
