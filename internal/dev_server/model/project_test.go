package model_test

import (
	"context"
	"errors"
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	adapters_mocks "github.com/launchdarkly/ldcli/internal/dev_server/adapters/mocks"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
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

	allFlags := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true)}).
		Build()

	t.Run("Returns error if it cant fetch flag state", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return("", errors.New("fetch flag state fails"))
		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch flag state fails", err.Error())
	})

	t.Run("Returns error if it fails to insert the project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlags, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(errors.New("insert fails"))

		_, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "insert fails", err.Error())
	})

	t.Run("Successfully creates project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlags, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)

		p, err := model.CreateProject(ctx, projKey, sourceEnvKey, nil)
		assert.Nil(t, err)

		expectedProj := model.Project{
			Key:                  projKey,
			SourceEnvironmentKey: sourceEnvKey,
			Context:              ldcontext.NewBuilder("user").Key("dev-environment").Build(),
			FlagState:            model.FromAllFlags(allFlags),
		}

		assert.Equal(t, expectedProj.Key, p.Key)
		assert.Equal(t, expectedProj.SourceEnvironmentKey, p.SourceEnvironmentKey)
		assert.Equal(t, expectedProj.Context, p.Context)
		assert.Equal(t, expectedProj.FlagState, p.FlagState)
	})
}

func TestUpdateProject(t *testing.T) {
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	ctx := model.ContextWithStore(context.Background(), store)
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, mockController)
	ldCtx := ldcontext.New(t.Name())
	newSrcEnv := "newEnv"

	proj := model.Project{
		Key:                  "projKey",
		SourceEnvironmentKey: "srcEnvKey",
		Context:              ldcontext.New(t.Name()),
	}

	t.Run("Returns error if GetDevProject fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&model.Project{}, errors.New("GetDevProject fails"))
		_, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "GetDevProject fails", err.Error())
	})

	t.Run("Passing in context triggers FetchFlagState, returns error if the fetch fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("", errors.New("FetchFlagState fails"))

		_, err := model.UpdateProject(ctx, proj.Key, &ldCtx, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "FetchFlagState fails", err.Error())
	})

	t.Run("Passing in sourceEnvironmentKey triggers FetchFlagState, returns error if UpdateProject fails", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, newSrcEnv).Return("sdkKey", nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "sdkKey").Return(flagstate.AllFlags{}, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(false, errors.New("UpdateProject fails"))

		_, err := model.UpdateProject(ctx, proj.Key, nil, &newSrcEnv)
		assert.NotNil(t, err)
		assert.Equal(t, "UpdateProject fails", err.Error())
	})

	t.Run("Returns error if project was not actually updated", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(false, nil)

		_, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		assert.NotNil(t, err)
		assert.Equal(t, "Project not updated", err.Error())
	})

	t.Run("Return successfully", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), proj.Key).Return(&proj, nil)
		store.EXPECT().UpdateProject(gomock.Any(), gomock.Any()).Return(true, nil)

		project, err := model.UpdateProject(ctx, proj.Key, nil, nil)
		assert.Nil(t, err)
		assert.Equal(t, proj, project)
	})
}

func TestFetchFlagState(t *testing.T) {
	ctx := context.Background()
	mockController := gomock.NewController(t)
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, mockController)
	allFlags := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true)}).
		Build()

	proj := model.Project{
		Key:                  "projKey",
		SourceEnvironmentKey: "srcEnvKey",
		Context:              ldcontext.New(t.Name()),
	}

	t.Run("Returns error if fails to fetch sdk key", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("", errors.New("sdk key fail"))

		_, err := proj.FetchFlagState(ctx)
		assert.NotNil(t, err)
		assert.Equal(t, "sdk key fail", err.Error())
	})

	t.Run("Returns error if fails to fetch flags state", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("key", nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "key").Return(flagstate.AllFlags{}, errors.New("fetch fail"))

		_, err := proj.FetchFlagState(ctx)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch fail", err.Error())
	})

	t.Run("Successfully fetches flag state", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), proj.Key, proj.SourceEnvironmentKey).Return("key", nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "key").Return(allFlags, nil)

		flagState, err := proj.FetchFlagState(ctx)
		assert.Nil(t, err)
		assert.Equal(t, model.FromAllFlags(allFlags), flagState)
	})
}
