package model_test

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	adapters_mocks "github.com/launchdarkly/ldcli/internal/dev_server/adapters/mocks"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func TestRestoreDb(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	observers := model.NewObservers()
	ctx = model.SetObserversOnContext(ctx, observers)
	ctx, _, _ = adapters_mocks.WithMockApiAndSdk(ctx, mockController)
	store := mocks.NewMockStore(mockController)
	ctx = model.ContextWithStore(ctx, store)
	projKey := "proj"
	sourceEnvKey := "env"

	proj := model.Project{
		Key:                  projKey,
		SourceEnvironmentKey: sourceEnvKey,
		Context:              ldcontext.New(t.Name()),
		AllFlagsState: map[string]model.FlagState{
			"boolFlag": {
				Version: 0,
				Value:   ldvalue.Bool(false),
			},
		},
	}

	t.Run("Returns error if restore fails", func(t *testing.T) {
		store.EXPECT().RestoreBackup(gomock.Any(), gomock.Any()).Return("", errors.New("restore failed"))

		err := model.RestoreDb(ctx, strings.NewReader(""))
		assert.NotNil(t, err)
	})

	t.Run("Notifies Projects if restore completes", func(t *testing.T) {
		store.EXPECT().RestoreBackup(gomock.Any(), gomock.Any()).Return("restore.db", nil)
		store.EXPECT().GetDevProjectKeys(gomock.Any()).Return([]string{projKey}, nil)
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(&proj, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), projKey).Return(model.Overrides{}, nil)
		observer := mocks.NewMockObserver(mockController)
		observer.EXPECT().Handle(model.SyncEvent{ProjectKey: projKey, AllFlagsState: proj.AllFlagsState})

		observers.RegisterObserver(observer)

		err := model.RestoreDb(ctx, strings.NewReader(""))
		require.NoError(t, err)
	})
}
