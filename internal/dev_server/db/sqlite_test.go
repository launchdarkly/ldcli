package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func TestDBFunctions(t *testing.T) {
	ctx := context.Background()
	dbName := "test.db"

	store, err := db.NewSqlite(ctx, dbName)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.Remove(dbName))
	}()

	ldContext := ldcontext.New(t.Name())
	now := time.Now()

	projects := []model.Project{
		{
			Key:                  "main-test-proj",
			SourceEnvironmentKey: "env-1",
			Context:              ldContext,
			LastSyncTime:         now,
			AllFlagsState: model.FlagsState{
				"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 2},
				"flag-2": model.FlagState{Value: ldvalue.String("cool"), Version: 2},
			},
			AvailableVariations: []model.FlagVariation{
				{
					FlagKey: "flag-1",
					Variation: model.Variation{
						Id:    "1",
						Value: ldvalue.Bool(true),
					},
				},
				{
					FlagKey: "flag-1",
					Variation: model.Variation{
						Id:    "2",
						Value: ldvalue.Bool(false),
					},
				},
				{
					FlagKey: "flag-2",
					Variation: model.Variation{
						Id:          "3",
						Description: lo.ToPtr("cool description"),
						Name:        lo.ToPtr("cool name"),
						Value:       ldvalue.String("Cool"),
					},
				},
			},
		},
		{
			Key:                  "proj-to-delete",
			SourceEnvironmentKey: "env-2",
			Context:              ldContext,
			LastSyncTime:         now,
			AllFlagsState: model.FlagsState{
				"flag-1": model.FlagState{Value: ldvalue.Int(123), Version: 2},
				"flag-2": model.FlagState{Value: ldvalue.Float64(99.99), Version: 2},
			},
			AvailableVariations: []model.FlagVariation{
				{
					FlagKey: "flag-1",
					Variation: model.Variation{
						Id:    "1",
						Value: ldvalue.Int(123),
					},
				},
				{
					FlagKey: "flag-2",
					Variation: model.Variation{
						Id:    "2",
						Value: ldvalue.Float64(99.99),
					},
				},
			},
		},
	}
	actualProjectKeys := make(map[string]bool, len(projects))

	for _, proj := range projects {
		err := store.InsertProject(ctx, proj)
		require.NoError(t, err)
		actualProjectKeys[proj.Key] = true
	}

	t.Run("InsertProject returns ErrAlreadyExists if the project already exists", func(t *testing.T) {
		err := store.InsertProject(ctx, projects[0])
		assert.Equal(t, model.ErrAlreadyExists, err)
	})

	t.Run("GetDevProjectKeys returns keys in projects", func(t *testing.T) {
		keys, err := store.GetDevProjectKeys(ctx)
		assert.NoError(t, err)
		assert.Len(t, keys, len(projects))

		for _, key := range keys {
			_, ok := actualProjectKeys[key]
			assert.True(t, ok)
		}
	})

	t.Run("GetDevProject returns ErrNotFound for fake project keys", func(t *testing.T) {
		p, err := store.GetDevProject(ctx, "THIS-DOES-NOT-EXIST")
		assert.Nil(t, p)
		assert.ErrorIs(t, err, model.ErrNotFound)
	})

	t.Run("GetDevProject returns project", func(t *testing.T) {
		expected := projects[0]
		p, err := store.GetDevProject(ctx, expected.Key)

		assert.NoError(t, err)
		assert.NotNil(t, p)
		assert.Equal(t, expected.Key, p.Key)
		assert.Equal(t, expected.AllFlagsState, p.AllFlagsState)
		assert.Equal(t, expected.SourceEnvironmentKey, p.SourceEnvironmentKey)
		assert.Equal(t, expected.Context, p.Context)
		assert.True(t, expected.LastSyncTime.Equal(p.LastSyncTime))
	})

	t.Run("GetAvailableVariations returns variations", func(t *testing.T) {
		availableVariations, err := store.GetAvailableVariationsForProject(ctx, projects[0].Key)
		require.NoError(t, err)
		require.Len(t, availableVariations, 2)
		flag1Variations := availableVariations["flag-1"]
		assert.Len(t, flag1Variations, 2)
		flag2Variations := availableVariations["flag-2"]
		assert.Len(t, flag2Variations, 1)

		expectedFlagVariations := projects[0].AvailableVariations
		assert.Equal(t, expectedFlagVariations[2].Id, flag2Variations[0].Id)
		assert.Equal(t, *expectedFlagVariations[2].Name, *flag2Variations[0].Name)
		assert.Equal(t, *expectedFlagVariations[2].Description, *flag2Variations[0].Description)
		assert.Equal(t, expectedFlagVariations[2].Value.String(), flag2Variations[0].Value.String())
		assert.Equal(t, ldvalue.StringType, flag2Variations[0].Value.Type())
		for _, variation := range flag1Variations {
			if variation.Value.BoolValue() {
				assert.Equal(t, expectedFlagVariations[0].Variation, variation)
			} else {
				assert.Equal(t, expectedFlagVariations[1].Variation, variation)
			}
		}
	})

	t.Run("UpdateProject updates flag state, sync time, context and source environment key", func(t *testing.T) {
		project := projects[0]
		project.Context = ldcontext.New(t.Name() + "blah")
		project.AllFlagsState = model.FlagsState{
			"flag-1": model.FlagState{Value: ldvalue.Bool(false), Version: 3},
			"flag-2": model.FlagState{Value: ldvalue.String("cool beeans"), Version: 3},
		}
		project.LastSyncTime = time.Now().Add(time.Hour)
		project.SourceEnvironmentKey = "new-env"
		project.AvailableVariations = []model.FlagVariation{
			{
				FlagKey: "flag-1",
				Variation: model.Variation{
					Id:    "1",
					Value: ldvalue.Bool(true),
				},
			},
			{
				FlagKey: "flag-1",
				Variation: model.Variation{
					Id:    "2",
					Value: ldvalue.Bool(false),
				},
			},
			{
				FlagKey: "flag-2",
				Variation: model.Variation{
					Id:          "3",
					Description: lo.ToPtr("cool description"),
					Name:        lo.ToPtr("cool name"),
					Value:       ldvalue.String("cool beans"),
				},
			},
		}

		updated, err := store.UpdateProject(ctx, project)
		assert.NoError(t, err)
		assert.True(t, updated)

		newProj, err := store.GetDevProject(ctx, project.Key)
		assert.NoError(t, err)
		assert.NotNil(t, newProj)
		assert.Equal(t, project.Key, newProj.Key)
		assert.Equal(t, project.AllFlagsState, newProj.AllFlagsState)
		assert.Equal(t, project.SourceEnvironmentKey, newProj.SourceEnvironmentKey)
		assert.Equal(t, project.Context, newProj.Context)
		assert.True(t, project.LastSyncTime.Equal(newProj.LastSyncTime))

		availableVariations, err := store.GetAvailableVariationsForProject(ctx, projects[0].Key)
		require.NoError(t, err)
		require.Len(t, availableVariations, 2)
		flag1Variations := availableVariations["flag-1"]
		assert.Len(t, flag1Variations, 2)
		flag2Variations := availableVariations["flag-2"]
		assert.Len(t, flag2Variations, 1)

		expectedFlagVariation := project.AvailableVariations[2]
		assert.Equal(t, expectedFlagVariation.Id, flag2Variations[0].Id)
		assert.Equal(t, *expectedFlagVariation.Name, *flag2Variations[0].Name)
		assert.Equal(t, *expectedFlagVariation.Description, *flag2Variations[0].Description)
		assert.Equal(t, expectedFlagVariation.Value.String(), flag2Variations[0].Value.String())
		assert.Equal(t, ldvalue.StringType, flag2Variations[0].Value.Type())
	})

	t.Run("UpdateProject returns false if project does not exist", func(t *testing.T) {
		updated, err := store.UpdateProject(ctx, model.Project{Key: "nope"})
		assert.NoError(t, err)
		assert.False(t, updated)
	})

	t.Run("DeleteProject returns false if project does not exist", func(t *testing.T) {
		deleted, err := store.DeleteDevProject(ctx, "nope")
		assert.NoError(t, err)
		assert.False(t, deleted)
	})

	t.Run("DeleteProject succeeds if project exists", func(t *testing.T) {
		deleted, err := store.DeleteDevProject(ctx, projects[1].Key)
		assert.NoError(t, err)
		assert.True(t, deleted)
	})

	flagKeys := []string{"flag-1", "flag-2"}

	overrides := map[string]model.Override{
		flagKeys[0]: {
			ProjectKey: projects[0].Key,
			FlagKey:    flagKeys[0],
			Value:      ldvalue.Bool(true),
			Active:     true,
			Version:    1,
		},
		flagKeys[1]: {
			ProjectKey: projects[0].Key,
			FlagKey:    flagKeys[1],
			Value:      ldvalue.Int(100),
			Active:     true,
			Version:    1,
		},
	}

	// test inserts
	for _, o := range overrides {
		_, err := store.UpsertOverride(ctx, o)
		require.NoError(t, err)
	}

	overridesResult, err := store.GetOverridesForProject(ctx, projects[0].Key)
	require.NoError(t, err)
	require.Len(t, overridesResult, 2)

	for _, r := range overridesResult {
		originalOverride, ok := overrides[r.FlagKey]
		require.True(t, ok)
		require.Equal(t, originalOverride, r)
	}

	t.Run("UpsertOverride updates when override exists", func(t *testing.T) {
		updated := overrides[flagKeys[1]]
		updated.Value = ldvalue.Int(101)

		_, err := store.UpsertOverride(ctx, updated)
		assert.NoError(t, err)

		overridesResult, err := store.GetOverridesForProject(ctx, projects[0].Key)
		assert.NoError(t, err)
		assert.Len(t, overridesResult, 2)

		found := false // prevent test from erroneously succeeding because override not in array
		for _, r := range overridesResult {
			if r.FlagKey != flagKeys[1] {
				continue
			}

			found = true
			assert.Equal(t, updated.Value, r.Value)
		}

		assert.True(t, found)
	})

	t.Run("DeactivateOverride returns error when override not found", func(t *testing.T) {
		err := store.DeactivateOverride(ctx, projects[0].Key, "nope")
		assert.ErrorIs(t, err, model.ErrNotFound)
	})

	t.Run("DeactivateOverride sets the override inactive", func(t *testing.T) {
		toDelete := overrides[flagKeys[0]]
		err := store.DeactivateOverride(ctx, toDelete.ProjectKey, toDelete.FlagKey)
		assert.NoError(t, err)

		result, err := store.GetOverridesForProject(ctx, toDelete.ProjectKey)
		assert.NoError(t, err)
		assert.Len(t, result, 2)

		found := false // prevent test from erroneously succeeding because override not in array
		for _, r := range result {
			if r.FlagKey != toDelete.FlagKey {
				continue
			}

			found = true
			assert.False(t, r.Active)
		}

		assert.True(t, found)
	})
}
