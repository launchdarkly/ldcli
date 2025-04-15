package model_test

import (
	"context"
	"testing"

	"github.com/pkg/errors"
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

func TestInitialSync(t *testing.T) {

	ctx := context.Background()
	mockController := gomock.NewController(t)
	observers := model.NewObservers()
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, mockController)
	store := mocks.NewMockStore(mockController)
	ctx = model.ContextWithStore(ctx, store)
	ctx = model.SetObserversOnContext(ctx, observers)
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

	t.Run("Returns no error if disabled", func(t *testing.T) {
		input := model.InitialProjectSettings{
			Enabled:    false,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			Overrides:  nil,
		}
		err := model.CreateOrSyncProject(ctx, input)
		assert.NoError(t, err)
	})

	t.Run("Returns error if it cant fetch flag state", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return("", errors.New("fetch flag state fails"))
		input := model.InitialProjectSettings{
			Enabled:    true,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			Overrides:  nil,
		}
		err := model.CreateOrSyncProject(ctx, input)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch flag state fails", err.Error())
	})

	t.Run("Returns error if it can't fetch flags", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(nil, errors.New("fetch flags failed"))
		input := model.InitialProjectSettings{
			Enabled:    true,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			Overrides:  nil,
		}
		err := model.CreateOrSyncProject(ctx, input)
		assert.NotNil(t, err)
		assert.Equal(t, "fetch flags failed", err.Error())
	})

	t.Run("Returns error if it fails to insert the project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(errors.New("insert fails"))

		input := model.InitialProjectSettings{
			Enabled:    true,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			Overrides:  nil,
		}
		err := model.CreateOrSyncProject(ctx, input)
		assert.NotNil(t, err)
		assert.Equal(t, "insert fails", err.Error())
	})

	t.Run("Successfully creates project", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)

		input := model.InitialProjectSettings{
			Enabled:    true,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			Overrides:  nil,
		}
		err := model.CreateOrSyncProject(ctx, input)

		assert.NoError(t, err)
	})
	t.Run("Successfully creates project with override", func(t *testing.T) {
		override := model.Override{
			ProjectKey: projKey,
			FlagKey:    "boolFlag",
			Value:      ldvalue.Bool(true),
			Active:     true,
			Version:    1,
		}

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

		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)
		store.EXPECT().UpsertOverride(gomock.Any(), override).Return(override, nil)
		store.EXPECT().GetDevProject(gomock.Any(), projKey).Return(&proj, nil)

		input := model.InitialProjectSettings{
			Enabled:    true,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			Overrides: map[string]model.FlagValue{
				"boolFlag": ldvalue.Bool(true),
			},
		}
		err := model.CreateOrSyncProject(ctx, input)

		assert.NoError(t, err)
	})

	t.Run("If SyncOnce is set and the project already exists, return early", func(t *testing.T) {
		api.EXPECT().GetSdkKey(gomock.Any(), projKey, sourceEnvKey).Return(sdkKey, nil)
		sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), sdkKey).Return(allFlagsState, nil)
		api.EXPECT().GetAllFlags(gomock.Any(), projKey).Return(allFlags, nil)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(model.NewErrAlreadyExists("project", projKey))

		input := model.InitialProjectSettings{
			Enabled:    true,
			ProjectKey: projKey,
			EnvKey:     sourceEnvKey,
			Context:    nil,
			SyncOnce:   true,
		}
		err := model.CreateOrSyncProject(ctx, input)

		assert.NoError(t, err)
	})

}
