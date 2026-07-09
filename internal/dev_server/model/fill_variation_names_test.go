package model_test

import (
	"context"
	"sync"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	adapters_mocks "github.com/launchdarkly/ldcli/internal/dev_server/adapters/mocks"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func TestFillVariationNames(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	ctx, api, _ := adapters_mocks.WithMockApiAndSdk(ctx, ctrl)
	store := mocks.NewMockStore(ctrl)
	ctx = model.ContextWithStore(ctx, store)

	projKey := "proj"
	strPtr := func(s string) *string { return &s }
	flag := func(key string) ldapi.FeatureFlag {
		return ldapi.FeatureFlag{
			Key: key,
			Variations: []ldapi.Variation{
				{Id: strPtr(key + "-0"), Name: strPtr("On"), Value: true},
				{Id: strPtr(key + "-1"), Name: strPtr("Off"), Value: false},
			},
		}
	}

	// A pending variation means work remains, so the fill proceeds.
	store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), projKey).
		Return(map[string][]model.Variation{"a": {{Id: "pending-0"}}}, nil)

	// total 250 with page size 100 => pages at offsets 0, 100, 200.
	api.EXPECT().GetFlagsPage(gomock.Any(), projKey, int64(100), int64(0)).
		Return([]ldapi.FeatureFlag{flag("a")}, 250, nil)
	api.EXPECT().GetFlagsPage(gomock.Any(), projKey, int64(100), int64(100)).
		Return([]ldapi.FeatureFlag{flag("b")}, 250, nil)
	api.EXPECT().GetFlagsPage(gomock.Any(), projKey, int64(100), int64(200)).
		Return([]ldapi.FeatureFlag{flag("c")}, 250, nil)

	var mu sync.Mutex
	upserted := map[string]bool{}
	store.EXPECT().UpsertAvailableVariationsForFlags(gomock.Any(), projKey, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, byFlagKey map[string][]model.Variation) error {
			mu.Lock()
			defer mu.Unlock()
			for key, variations := range byFlagKey {
				assert.Len(t, variations, 2)
				assert.Equal(t, "On", *variations[0].Name)
				upserted[key] = true
			}
			return nil
		}).Times(3)

	model.FillVariationNames(ctx, projKey)

	assert.Equal(t, map[string]bool{"a": true, "b": true, "c": true}, upserted)
}

func TestFillVariationNamesInitialPageError(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	ctx, api, _ := adapters_mocks.WithMockApiAndSdk(ctx, ctrl)
	store := mocks.NewMockStore(ctrl)
	ctx = model.ContextWithStore(ctx, store)

	store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), "proj").
		Return(map[string][]model.Variation{"a": {{Id: "pending-0"}}}, nil)

	// A failed first page bails out entirely - no upserts, no panic.
	api.EXPECT().GetFlagsPage(gomock.Any(), "proj", int64(100), int64(0)).
		Return(nil, 0, assert.AnError)

	model.FillVariationNames(ctx, "proj")
}

func TestFillVariationNamesSkipsWhenResolved(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	ctx, _, _ = adapters_mocks.WithMockApiAndSdk(ctx, ctrl)
	store := mocks.NewMockStore(ctrl)
	ctx = model.ContextWithStore(ctx, store)

	// Every variation already has a real id, so the fill makes no REST calls.
	strPtr := func(s string) *string { return &s }
	store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), "proj").
		Return(map[string][]model.Variation{"a": {{Id: "abc", Name: strPtr("On")}}}, nil)

	model.FillVariationNames(ctx, "proj")
}
