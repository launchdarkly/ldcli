package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	adapters_mocks "github.com/launchdarkly/ldcli/internal/dev_server/adapters/mocks"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func TestCreateProject(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, mockController)
	store := mocks.NewMockStore(mockController)
	ctx = model.ContextWithStore(ctx, store)
	projKey := "proj"
	sourceEnvKey := "env"
	sdkKey := "thing"

	allFlagsState := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true)}).
		Build()

	variationsByFlagKey := map[string][]ldvalue.Value{
		"boolFlag": {ldvalue.Bool(true), ldvalue.Bool(false)},
	}

	t.Run("Returns error if it cant fetch flag state", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return("", errors.New("fetch flag state fails"))
		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch flag state fails", err.Error())
	})

	t.Run("Returns error if GetAllFlagsState fails", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).
			Return(flagstate.AllFlags{}, nil, errors.New("stream failed"))
		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "stream failed", err.Error())
	})

	t.Run("Returns error if it fails to insert the project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, variationsByFlagKey, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(errors.New("insert fails"))

		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "insert fails", err.Error())
	})

	t.Run("Successfully creates project, with values-only variations from streaming", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, variationsByFlagKey, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)

		p, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.Nil(t, err)

		expectedProj := model.Project{
			Key:                  projKey,
			SourceEnvironmentKey: sourceEnvKey,
			Context:              ldcontext.NewBuilder("user").Key("dev-environment").Build(),
			AllFlagsState:        model.FromAllFlags(allFlagsState),
		}

		assert.Equal(t, expectedProj.Key, p.Key)
		assert.Equal(t, expectedProj.SourceEnvironmentKey, p.SourceEnvironmentKey)
		assert.Equal(t, expectedProj.Context, p.Context)
		assert.Equal(t, expectedProj.AllFlagsState, p.AllFlagsState)

		require.Len(t, p.AvailableVariations, 2)
		seenIds := map[string]bool{}
		for _, v := range p.AvailableVariations {
			assert.Equal(t, "boolFlag", v.FlagKey)
			assert.NotEmpty(t, v.Id, "needs a unique placeholder id until a real one is resolved")
			assert.False(t, seenIds[v.Id], "placeholder ids must be unique per flag")
			seenIds[v.Id] = true
			assert.Nil(t, v.Name, "no REST call was made, so no name is known yet")
		}
	})
}

func TestUpdateProject(t *testing.T) {
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	ctx := model.ContextWithStore(context.Background(), store)
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, mockController)

	observer := mocks.NewMockObserver(mockController)
	observers := model.NewObservers()
	observers.RegisterObserver(observer)
	ctx = model.SetObserversOnContext(ctx, observers)

	ldCtx := ldcontext.New(t.Name())
	newSrcEnv := "newEnv"

	proj := model.Project{
		Key:                  "projKey",
		SourceEnvironmentKey: "srcEnvKey",
		Context:              ldcontext.New(t.Name()),
	}

	allFlagsState := flagstate.NewAllFlagsBuilder().
		AddFlag("stringFlag", flagstate.FlagState{Value: ldvalue.String("cool")}).
		Build()

	variationsByFlagKey := map[string][]ldvalue.Value{
		"stringFlag": {ldvalue.String("cool")},
	}

	t.Run("Returns error if GetDevProject fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&model.Project{}, errors.New("GetDevProject fails"))
		_, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "GetDevProject fails", err.Error())
	})

	t.Run("returns error if the fetch flag state fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("", errors.New("FetchFlagState fails"))

		_, err := model.UpdateProject(ctx, proj.Key, &ldCtx, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "FetchFlagState fails", err.Error())
	})

	t.Run("Returns error if UpdateProject fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, newSrcEnv).Return("sdkKey", nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "sdkKey").Return(allFlagsState, variationsByFlagKey, nil)
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), proj.Key).Return(map[string][]model.Variation{}, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(false, errors.New("UpdateProject fails"))

		_, err := model.UpdateProject(ctx, proj.Key, nil, &newSrcEnv)
		assert.NotNil(t, err)
		assert.Equal(t, "UpdateProject fails", err.Error())
	})

	t.Run("Returns error if project was not actually updated", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("sdkKey", nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "sdkKey").Return(allFlagsState, variationsByFlagKey, nil)
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), proj.Key).Return(map[string][]model.Variation{}, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(false, nil)

		_, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "Project not updated", err.Error())
	})

	t.Run("Return successfully, carrying over a previously resolved name", func(t *testing.T) {
		existingName := "Cool"
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("sdkKey", nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "sdkKey").Return(allFlagsState, variationsByFlagKey, nil)
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), proj.Key).Return(map[string][]model.Variation{
			"stringFlag": {{Id: "abc", Name: &existingName, Value: ldvalue.String("cool")}},
		}, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, p model.Project) (bool, error) {
				require.Len(t, p.AvailableVariations, 1)
				assert.Equal(t, "abc", p.AvailableVariations[0].Id)
				assert.Equal(t, &existingName, p.AvailableVariations[0].Name)
				return true, nil
			})
		store.EXPECT().IncrementProjectPayloadVersion(gomock.Any(), proj.Key).Return(2, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), proj.Key).Return(model.Overrides{}, nil)
		observer.
			EXPECT().
			Handle(model.SyncEvent{
				ProjectKey:     proj.Key,
				AllFlagsState:  model.FromAllFlags(allFlagsState),
				PayloadVersion: 2,
			})

		project, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		require.Nil(t, err)
		expectedProj := proj
		expectedProj.PayloadVersion = 2
		expectedProj.AvailableVariations = project.AvailableVariations // asserted above via DoAndReturn
		assert.Equal(t, expectedProj, project)
	})
}

func TestGetFlagStateWithOverridesForProject(t *testing.T) {
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	ctx := model.ContextWithStore(context.Background(), store)
	flagKey := "flg"
	proj := model.Project{
		Key:           "projKey",
		AllFlagsState: model.FlagsState{flagKey: model.FlagState{Value: ldvalue.Bool(false), Version: 1}},
	}

	t.Run("Returns error if store fetch fails", func(t *testing.T) {
		store.EXPECT().GetOverridesForProject(gomock.Any(), proj.Key).Return(model.Overrides{}, errors.New("fetch fails"))

		_, err := proj.GetFlagStateWithOverridesForProject(ctx)
		assert.NotNil(t, err)
		assert.Equal(t, "unable to fetch overrides for project projKey: fetch fails", err.Error())
	})

	t.Run("Returns flag state with overrides successfully", func(t *testing.T) {
		overrides := model.Overrides{
			{
				ProjectKey: proj.Key,
				FlagKey:    flagKey,
				Value:      ldvalue.Bool(true),
				Active:     true,
				Version:    1,
			},
		}

		store.EXPECT().GetOverridesForProject(gomock.Any(), proj.Key).Return(overrides, nil)

		withOverrides, err := proj.GetFlagStateWithOverridesForProject(ctx)
		assert.Nil(t, err)

		assert.Len(t, withOverrides, 1)

		overriddenFlag, exists := withOverrides[flagKey]
		assert.True(t, exists)
		assert.True(t, overriddenFlag.Value.BoolValue())
		assert.Equal(t, 2, overriddenFlag.Version)
	})
}
