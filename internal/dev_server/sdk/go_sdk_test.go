package sdk

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	ldclient "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters/mocks"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

// TestSdkRoutesViaGoSDK is an integration test. It hooks up a real go SDK to our SDK routes and makes changes to the
// model using the model methods directly. It also uses a real sqlite store. Goal of these tests are to ensure that the
// happy path works end to end (almost -- API handlers are omitted, but they are thin wrappers over the model).
func TestSDKRoutesViaGoSDK(t *testing.T) {
	const projectKey = "test-project"
	const environmentKey = "test-environment"
	const testSdkKey = "1234"

	// Wire up model dependencies to context
	ctx := context.Background()
	store, err := db.NewSqlite(ctx, "test.db")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Remove("test.db"))
	}()
	ctx = model.ContextWithStore(ctx, store)
	observers := model.NewObservers()
	ctx = model.SetObserversOnContext(ctx, observers)
	// Mock the external LD APIs
	mockController := gomock.NewController(t)
	ctx, api, sdk := mocks.WithMockApiAndSdk(ctx, mockController)

	api.EXPECT().GetSdkKey(gomock.Any(), projectKey, environmentKey).Return(testSdkKey, nil).AnyTimes()
	api.EXPECT().GetAllFlags(gomock.Any(), projectKey).
		Return(nil, nil). // Available variations are not used for evaluation
		AnyTimes()

	// Wire up sdk routes in test server
	router := mux.NewRouter()
	router.Use(model.StoreMiddleware(store))
	router.Use(model.ObserversMiddleware(observers))
	BindRoutes(router)
	require.NoError(t, err)
	testServer := httptest.NewServer(router)

	// Initialize project with all kinds of flags
	allFlags := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true)}).
		AddFlag("stringFlag", flagstate.FlagState{Value: ldvalue.String("cool")}).
		AddFlag("intFlag", flagstate.FlagState{Value: ldvalue.Int(123)}).
		AddFlag("doubleFlag", flagstate.FlagState{Value: ldvalue.Float64(99.99)}).
		AddFlag("jsonFlag", flagstate.FlagState{Value: ldvalue.CopyArbitraryValue(map[string]any{"cat": "hat"})}).
		Build()

	sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), testSdkKey).Return(allFlags, nil)
	_, err = model.CreateProject(ctx, projectKey, environmentKey, nil)
	require.NoError(t, err)

	// Configure go SDK to use test server
	ldConfig := ldclient.Config{}
	ldConfig.ServiceEndpoints.Streaming = testServer.URL
	ldConfig.ServiceEndpoints.Events = testServer.URL
	ldConfig.ServiceEndpoints.Polling = testServer.URL
	ld, err := ldclient.MakeCustomClient(projectKey, ldConfig, time.Second)
	require.NoError(t, err)

	ldContext := ldcontext.New(t.Name())

	t.Run("bool flag is true in fresh environment", func(t *testing.T) {
		val, err := ld.BoolVariation("boolFlag", ldContext, false)
		require.NoError(t, err)
		assert.True(t, val, "boolean variation is expected value")
	})

	t.Run("string flag is cool in fresh environment", func(t *testing.T) {
		val, err := ld.StringVariation("stringFlag", ldContext, "bad")
		require.NoError(t, err)
		assert.Equal(t, "cool", val)
	})

	t.Run("int flag is 123 in fresh environment", func(t *testing.T) {
		val, err := ld.IntVariation("intFlag", ldContext, 0)
		require.NoError(t, err)
		assert.Equal(t, 123, val)
	})

	t.Run("doubleFlag is 4 9s in fresh environment", func(t *testing.T) {
		val, err := ld.Float64Variation("doubleFlag", ldContext, 0)
		require.NoError(t, err)
		assert.Equal(t, 99.99, val)
	})

	t.Run("jsonFlag is cat :hat in fresh environment", func(t *testing.T) {
		val, err := ld.JSONVariation("jsonFlag", ldContext, ldvalue.Null())
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"cat": "hat"}, val.AsArbitraryValue())
	})

	// Mock scenario: we re-sync and the SDK returns new values and higher version numbers
	updatedFlags := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(false), Version: 2}).
		AddFlag("stringFlag", flagstate.FlagState{Value: ldvalue.String("pool"), Version: 2}).
		AddFlag("intFlag", flagstate.FlagState{Value: ldvalue.Int(789), Version: 2}).
		AddFlag("doubleFlag", flagstate.FlagState{Value: ldvalue.Float64(101.01), Version: 2}).
		AddFlag("jsonFlag", flagstate.FlagState{Value: ldvalue.CopyArbitraryValue(map[string]any{"cat": "bababooey"}), Version: 2}).
		Build()
	valuesMap := updatedFlags.ToValuesMap()

	sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), testSdkKey).Return(updatedFlags, nil)

	// This test is testing the "put" payload in a roundabout way by verifying each of the flags are in there.
	t.Run("Sync sends full flag payload for project", func(t *testing.T) {
		trackers := make(map[string]<-chan interfaces.FlagValueChangeEvent, len(valuesMap))

		for flagKey := range valuesMap {
			flagUpdateChan := ld.GetFlagTracker().AddFlagValueChangeListener(flagKey, ldContext, ldvalue.String("uh-oh"))
			defer ld.GetFlagTracker().RemoveFlagValueChangeListener(flagUpdateChan)
			trackers[flagKey] = flagUpdateChan
		}

		_, err := model.UpdateProject(ctx, projectKey, nil, nil)
		require.NoError(t, err)

		for flagKey, value := range valuesMap {
			updateTracker, ok := trackers[flagKey]
			require.True(t, ok)

			update := <-updateTracker
			assert.Equal(t, value.AsArbitraryValue(), update.NewValue.AsArbitraryValue())
		}
	})

	updates := map[string]ldvalue.Value{
		"boolFlag":   ldvalue.Bool(true),
		"stringFlag": ldvalue.String("drool"),
		"intFlag":    ldvalue.Int(456),
		"doubleFlag": ldvalue.Float64(88.88),
		"jsonFlag":   ldvalue.CopyArbitraryValue(map[string]any{"tortoise": "shell"}),
	}
	for flagKey, value := range updates {
		t.Run(fmt.Sprintf("%s is %v after override", flagKey, value), func(t *testing.T) {
			flagUpdateChan := ld.GetFlagTracker().AddFlagValueChangeListener(flagKey, ldContext, ldvalue.String("uh-oh"))
			defer ld.GetFlagTracker().RemoveFlagValueChangeListener(flagUpdateChan)
			_, err := model.UpsertOverride(ctx, projectKey, flagKey, value)
			require.NoError(t, err)
			flagUpdate := <-flagUpdateChan
			assert.Equal(t, value.AsArbitraryValue(), flagUpdate.NewValue.AsArbitraryValue())
		})
	}
}
