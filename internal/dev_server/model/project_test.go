package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	ldapi "github.com/launchdarkly/api-client-go/v14"
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

	trueVariationId, falseVariationId := "true", "false"
	allFlags := []ldapi.FeatureFlag{{
		Name: "bool flag",
		Kind: "bool",
		Key:  "boolFlag",
		Variations: []ldapi.Variation{
			{
				Id:    &trueVariationId,
				Value: true,
			},
			{
				Id:    &falseVariationId,
				Value: false,
			},
		},
	}}

	t.Run("Returns error if it cant fetch flag state", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return("", errors.New("fetch flag state fails"))
		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch flag state fails", err.Error())
	})

	t.Run("Returns error if it can't fetch flags", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(nil, errors.New("fetch flags failed"))
		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch flags failed", err.Error())
	})

	t.Run("Returns error if it fails to insert the project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		api.EXPECT().GetProjectEnvironments(gomock.Any(), projKey, "", nil).Return([]ldapi.Environment{
			{
				Key: sourceEnvKey,
				Id:  "test-client-side-id",
			},
		}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(errors.New("insert fails"))

		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.Equal(t, "insert fails", err.Error())
	})

	t.Run("Successfully creates a project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		api.EXPECT().GetProjectEnvironments(gomock.Any(), projKey, "", nil).Return([]ldapi.Environment{
			{
				Key: sourceEnvKey,
				Id:  "test-client-side-id",
			},
		}, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)

		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NoError(t, err)
	})
}

func TestUpdateProject(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	observers := model.NewObservers()
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, mockController)
	store := mocks.NewMockStore(mockController)
	ctx = model.ContextWithStore(ctx, store)
	ctx = model.SetObserversOnContext(ctx, observers)
	projKey := "projKey"
	sourceEnvKey := "env"
	sdkKey := "sdkKey"

	allFlagsState := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true)}).
		Build()
	trueVariationId, falseVariationId := "true", "false"
	allFlags := []ldapi.FeatureFlag{{
		Name: "bool flag",
		Kind: "bool",
		Key:  "boolFlag",
		Variations: []ldapi.Variation{
			{
				Id:    &trueVariationId,
				Value: true,
			},
			{
				Id:    &falseVariationId,
				Value: false,
			},
		},
	}}

	proj := model.Project{
		Key:                  projKey,
		SourceEnvironmentKey: sourceEnvKey,
		Context:              ldcontext.New("user"),
		AllFlagsState:        model.FromAllFlags(allFlagsState),
	}

	t.Run("Returns error if it can't find the project", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(nil, errors.New("not found"))

		_, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		assert.Equal(t, "not found", err.Error())
	})

	t.Run("Returns error if UpdateProject fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		api.EXPECT().GetProjectEnvironments(gomock.Any(), projKey, "", nil).Return([]ldapi.Environment{
			{
				Key: sourceEnvKey,
				Id:  "test-client-side-id",
			},
		}, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(false, errors.New("update fails"))

		_, err := model.UpdateProject(ctx, projKey, nil, nil)
		assert.Equal(t, "update fails", err.Error())
	})

	t.Run("Successfully updates a project", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		api.EXPECT().GetProjectEnvironments(gomock.Any(), projKey, "", nil).Return([]ldapi.Environment{
			{
				Key: sourceEnvKey,
				Id:  "test-client-side-id",
			},
		}, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(true, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), projKey).Return(model.Overrides{}, nil)

		_, err := model.UpdateProject(ctx, projKey, nil, nil)
		assert.NoError(t, err)
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
