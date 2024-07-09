package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			Key:                  "proj-1",
			SourceEnvironmentKey: "env-1",
			Context:              ldContext,
			LastSyncTime:         now,
			AllFlagsState: model.FlagsState{
				"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 2},
				"flag-2": model.FlagState{Value: ldvalue.String("cool"), Version: 2},
			},
		},
		{
			Key:                  "proj-2",
			SourceEnvironmentKey: "env-2",
			Context:              ldContext,
			LastSyncTime:         now,
			AllFlagsState: model.FlagsState{
				"flag-1": model.FlagState{Value: ldvalue.Int(123), Version: 2},
				"flag-2": model.FlagState{Value: ldvalue.Float64(99.99), Version: 2},
			},
		},
	}
	actualProjectKeys := make(map[string]bool, len(projects))

	for _, proj := range projects {
		err := store.InsertProject(ctx, proj)
		require.NoError(t, err)
		actualProjectKeys[proj.Key] = true
	}

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

	t.Run("UpdateProject updates flag state, sync time, context but not source environment key", func(t *testing.T) {
		projects[0].Context = ldcontext.New(t.Name() + "blah")
		projects[0].AllFlagsState = model.FlagsState{
			"flag-1": model.FlagState{Value: ldvalue.Bool(false), Version: 3},
			"flag-2": model.FlagState{Value: ldvalue.String("cool beeans"), Version: 3},
		}
		projects[0].LastSyncTime = time.Now().Add(time.Hour)
		oldSourceEnvKey := projects[0].SourceEnvironmentKey
		projects[0].SourceEnvironmentKey = "new-env"

		updated, err := store.UpdateProject(ctx, projects[0])
		assert.NoError(t, err)
		assert.True(t, updated)

		newProj, err := store.GetDevProject(ctx, projects[0].Key)
		assert.NoError(t, err)
		assert.NotNil(t, newProj)
		assert.Equal(t, projects[0].Key, newProj.Key)
		assert.Equal(t, projects[0].AllFlagsState, newProj.AllFlagsState)
		assert.Equal(t, oldSourceEnvKey, newProj.SourceEnvironmentKey)
		assert.Equal(t, projects[0].Context, newProj.Context)
		assert.True(t, projects[0].LastSyncTime.Equal(newProj.LastSyncTime))
	})

	t.Run("UpdateProject returns false if project does not exist", func(t *testing.T) {
		updated, err := store.UpdateProject(ctx, model.Project{Key: "nope"})
		assert.NoError(t, err)
		assert.False(t, updated)
	})
}
