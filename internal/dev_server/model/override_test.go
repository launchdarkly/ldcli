package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestUpsertOverride(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	projKey := "proj"
	flagKey := "flg"
	ldValue := ldvalue.Bool(true)
	override := model.Override{
		ProjectKey: projKey,
		FlagKey:    flagKey,
		Value:      ldValue,
		Active:     true,
		Version:    1,
	}

	project := &model.Project{
		Key:           projKey,
		AllFlagsState: model.FlagsState{flagKey: model.FlagState{Value: ldvalue.Bool(false), Version: 1}},
	}

	ctx = model.ContextWithStore(ctx, store)

	observers := model.NewObservers()
	observer := mocks.NewMockObserver(mockController)

	observers.RegisterObserver(observer)
	ctx = model.SetObserversOnContext(ctx, observers)

	t.Run("store unable to get project, returns error", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(nil, errors.New("test 2"))

		_, err := model.UpsertOverride(ctx, projKey, flagKey, ldValue)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "project does not exist within dev server")
	})

	t.Run("Returns error if flag does not exist in project", func(t *testing.T) {
		badProj := model.Project{
			Key:           projKey,
			AllFlagsState: model.FlagsState{},
		}
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(&badProj, nil)

		_, err := model.UpsertOverride(ctx, projKey, flagKey, ldValue)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "flag does not exist within dev project")
	})

	t.Run("store fails to upsert, returns error", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(project, nil)
		store.EXPECT().UpsertOverride(gomock.Any(), gomock.Any()).Return(model.Override{}, errors.New("testy test"))

		_, err := model.UpsertOverride(ctx, projKey, flagKey, ldValue)
		assert.Error(t, err)
		assert.Equal(t, "testy test", err.Error())
	})

	t.Run("override is applied, observers are notified", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(project, nil)
		store.EXPECT().UpsertOverride(gomock.Any(), override).Return(override, nil)
		observer.
			EXPECT().
			Handle(model.OverrideEvent{
				FlagKey:    flagKey,
				ProjectKey: projKey,
				FlagState:  model.FlagState{Value: ldvalue.Bool(true), Version: 2},
			})

		o, err := model.UpsertOverride(ctx, projKey, flagKey, ldValue)
		assert.Nil(t, err)
		assert.Equal(t, override, o)
	})
}

func TestOverrideApply(t *testing.T) {
	projKey := "proj"
	flagKey := "flg"
	ldValue := ldvalue.Bool(true)
	oldState := model.FlagState{Value: ldvalue.Bool(false), Version: 1}

	t.Run("if override is inactive, increment version", func(t *testing.T) {
		override := model.Override{
			ProjectKey: projKey,
			FlagKey:    flagKey,
			Value:      ldValue,
			Version:    1,
		}

		state := override.Apply(oldState)
		assert.False(t, state.Value.BoolValue())
		assert.Equal(t, 2, state.Version)
	})

	t.Run("if override is active, increment version AND update value", func(t *testing.T) {
		override := model.Override{
			ProjectKey: projKey,
			FlagKey:    flagKey,
			Value:      ldValue,
			Active:     true,
			Version:    1,
		}

		state := override.Apply(oldState)
		assert.True(t, state.Value.BoolValue())
		assert.Equal(t, 2, state.Version)
	})
}
