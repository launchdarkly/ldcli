package model_test

import (
	"context"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	adapters_mocks "github.com/launchdarkly/ldcli/internal/dev_server/adapters/mocks"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func strPtr(s string) *string { return &s }

func TestFillVariations(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	ctx, api, _ := adapters_mocks.WithMockApiAndSdk(ctx, ctrl)
	store := mocks.NewMockStore(ctrl)
	ctx = model.ContextWithStore(ctx, store)

	api.EXPECT().GetAllFlags(gomock.Any(), "proj").Return([]ldapi.FeatureFlag{{
		Key: "boolFlag",
		Variations: []ldapi.Variation{
			{Id: strPtr("t"), Name: strPtr("On"), Value: true},
			{Id: strPtr("f"), Name: strPtr("Off"), Value: false},
		},
	}}, nil)
	store.EXPECT().SetAvailableVariationsForProject(gomock.Any(), "proj", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, vars []model.FlagVariation) error {
			require.Len(t, vars, 2)
			assert.Equal(t, "t", vars[0].Id)
			require.NotNil(t, vars[0].Name)
			assert.Equal(t, "On", *vars[0].Name)
			return nil
		})

	model.FillVariations(ctx, "proj")
}

// Streaming mode must not block on the REST fetch: it keeps existing variations and schedules a background fill after overrides are applied.
func TestStreamStartupDefersVariationFetch(t *testing.T) {
	filled := make(chan string, 1)
	original := model.FillVariationsAsync
	model.FillVariationsAsync = func(_ context.Context, projectKey string) { filled <- projectKey }
	defer func() { model.FillVariationsAsync = original }()

	ctx := context.Background()
	ctrl := gomock.NewController(t)
	ctx, api, sdk := adapters_mocks.WithMockApiAndSdk(ctx, ctrl)
	store := mocks.NewMockStore(ctrl)
	ctx = model.ContextWithStore(ctx, store)
	ctx = model.SetObserversOnContext(ctx, model.NewObservers())
	ctx = model.WithStreamStartup(ctx, true)

	allFlagsState := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true)}).
		Build()

	api.EXPECT().GetSdkKey(gomock.Any(), "proj", "env").Return("sdk", nil)
	sdk.EXPECT().GetAllFlagsState(gomock.Any(), gomock.Any(), "sdk").Return(allFlagsState, nil)
	// Stream mode preserves existing variations; GetAllFlags has no expectation, so the mock fails if it's called here.
	store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), "proj").Return(map[string][]model.Variation{}, nil)
	store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)

	err := model.CreateOrSyncProject(ctx, model.InitialProjectSettings{
		Enabled: true, ProjectKey: "proj", EnvKey: "env",
	})
	require.NoError(t, err)

	select {
	case pk := <-filled:
		assert.Equal(t, "proj", pk, "the background fill should be scheduled for the project")
	default:
		t.Fatal("expected a background variation fill to be scheduled in streaming mode")
	}
}
