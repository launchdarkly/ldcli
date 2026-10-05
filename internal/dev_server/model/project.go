package model

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	ldapi "github.com/launchdarkly/api-client-go/v14"
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
	PayloadVersion       int
}

// CreateProject creates a project and adds it to the database.
func CreateProject(ctx context.Context, projectKey, sourceEnvironmentKey string, ldCtx *ldcontext.Context) (Project, error) {
	project := Project{
		Key:                  projectKey,
		SourceEnvironmentKey: sourceEnvironmentKey,
		PayloadVersion:       1,
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

	if StreamStartupFromContext(ctx) {
		// Defer the REST fetch to a background fill; keep the stored variations so this write doesn't blank the dropdown, and report healthy now.
		existing, err := StoreFromContext(ctx).GetAvailableVariationsForProject(ctx, project.Key)
		if err != nil {
			return err
		}
		project.AvailableVariations = flattenVariations(existing)
		return nil
	}

	availableVariations, err := project.fetchAvailableVariations(ctx)
	if err != nil {
		return err
	}
	project.AvailableVariations = availableVariations
	return nil
}

// flattenVariations turns the store's per-flag variation map into a flat slice.
func flattenVariations(byFlagKey map[string][]Variation) []FlagVariation {
	var all []FlagVariation
	for flagKey, variations := range byFlagKey {
		for _, variation := range variations {
			all = append(all, FlagVariation{FlagKey: flagKey, Variation: variation})
		}
	}
	return all
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

	newPayloadVersion, err := store.IncrementProjectPayloadVersion(ctx, projectKey)
	if err != nil {
		return Project{}, errors.Wrap(err, "unable to increment payload version")
	}
	project.PayloadVersion = newPayloadVersion

	allFlagsWithOverrides, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		return Project{}, errors.Wrapf(err, "unable to get overrides for project, %s", projectKey)
	}

	GetObserversFromContext(ctx).Notify(SyncEvent{
		ProjectKey:     project.Key,
		AllFlagsState:  allFlagsWithOverrides,
		PayloadVersion: project.PayloadVersion,
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
	flags, err := adapters.GetApi(ctx).GetAllFlags(ctx, project.Key)
	if err != nil {
		return nil, err
	}
	return variationsFromFlags(flags), nil
}

// variationsFromFlags flattens REST flags into stored variations.
func variationsFromFlags(flags []ldapi.FeatureFlag) []FlagVariation {
	var allVariations []FlagVariation
	for _, flag := range flags {
		for i, variation := range flag.Variations {
			// Guard the nil id: fall back to a unique per-index id, never deref nil or leave an empty id (which would collide).
			id := fmt.Sprintf("variation-%d", i)
			if variation.Id != nil {
				id = *variation.Id
			}
			allVariations = append(allVariations, FlagVariation{
				FlagKey: flag.Key,
				Variation: Variation{
					Id:          id,
					Description: variation.Description,
					Name:        variation.Name,
					Value:       ldvalue.CopyArbitraryValue(variation.Value),
				},
			})
		}
	}
	return allVariations
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
