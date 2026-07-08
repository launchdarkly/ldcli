package model

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
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
	flagsState, variations, err := project.fetchFlagStateAndVariations(ctx)
	if err != nil {
		return Project{}, err
	}
	project.AllFlagsState = flagsState
	project.AvailableVariations = variations
	project.LastSyncTime = time.Now()

	store := StoreFromContext(ctx)
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

	flagsState, variations, err := project.fetchFlagStateAndVariations(ctx)
	if err != nil {
		return Project{}, err
	}
	project.AllFlagsState = flagsState
	project.LastSyncTime = time.Now()

	// Streaming never carries names, so keep any names already resolved for
	// a variation instead of wiping them out on every resync.
	existing, err := store.GetAvailableVariationsForProject(ctx, projectKey)
	if err != nil {
		return Project{}, err
	}
	project.AvailableVariations = mergeVariationNames(variations, existing)

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

// fetchFlagStateAndVariations gets flag state and variation values off one
// streaming connection. Values only, no name/description - that only comes
// from the REST API, resolved lazily elsewhere (see ResolveVariationNames).
func (project Project) fetchFlagStateAndVariations(ctx context.Context) (FlagsState, []FlagVariation, error) {
	apiAdapter := adapters.GetApi(ctx)
	sdkKey, err := apiAdapter.GetSdkKey(ctx, project.Key, project.SourceEnvironmentKey)
	flagsState := make(FlagsState)
	if err != nil {
		return flagsState, nil, err
	}

	sdkAdapter := adapters.GetSdk(ctx)
	sdkFlags, variationsByFlagKey, err := sdkAdapter.GetAllFlagsState(ctx, project.Context, sdkKey)
	if err != nil {
		return flagsState, nil, err
	}

	flagsState = FromAllFlags(sdkFlags)

	var variations []FlagVariation
	for flagKey, values := range variationsByFlagKey {
		for i, value := range values {
			variations = append(variations, FlagVariation{
				FlagKey: flagKey,
				Variation: Variation{
					// Placeholder id, unique per flag+index. Storage requires
					// a non-empty, per-flag-unique id (UNIQUE(project_key,
					// flag_key, id) ON CONFLICT REPLACE) - two real empty ids
					// would silently overwrite each other. ResolveVariationNames
					// replaces this with the real REST id once resolved.
					Id:    fmt.Sprintf("pending-%d", i),
					Value: value,
				},
			})
		}
	}
	return flagsState, variations, nil
}

// mergeVariationNames carries over name/description from existing into
// fresh wherever a flag+value match, so a resync doesn't wipe out names
// resolved by an earlier lazy lookup (streaming never has names, so fresh
// always comes in nameless).
func mergeVariationNames(fresh []FlagVariation, existingByFlagKey map[string][]Variation) []FlagVariation {
	merged := make([]FlagVariation, len(fresh))
	for i, fv := range fresh {
		merged[i] = fv
		for _, existing := range existingByFlagKey[fv.FlagKey] {
			if existing.Value.Equal(fv.Value) {
				merged[i].Id = existing.Id
				merged[i].Name = existing.Name
				merged[i].Description = existing.Description
				break
			}
		}
	}
	return merged
}
