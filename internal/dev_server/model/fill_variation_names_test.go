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

	// First read is the gate; second is the post-pass reconcile (all resolved => nothing to mark).
	gomock.InOrder(
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), projKey).
			Return(map[string][]model.Variation{"a": {{Id: "pending-0"}}}, nil),
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), projKey).
			Return(map[string][]model.Variation{"a": {{Id: "a-0"}}, "b": {{Id: "b-0"}}, "c": {{Id: "c-0"}}}, nil),
	)

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

func TestFillVariationNamesMarksUnresolvable(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	ctx, api, _ := adapters_mocks.WithMockApiAndSdk(ctx, ctrl)
	store := mocks.NewMockStore(ctrl)
	ctx = model.ContextWithStore(ctx, store)

	strPtr := func(s string) *string { return &s }

	gomock.InOrder(
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), "proj").
			Return(map[string][]model.Variation{"exotic": {{Id: "pending-0"}}}, nil),
		// After a clean pass "exotic" is still pending - the list never returned it.
		store.EXPECT().GetAvailableVariationsForProject(gomock.Any(), "proj").
			Return(map[string][]model.Variation{"exotic": {{Id: "pending-0"}}}, nil),
	)

	// Single page (total 1), and the list doesn't include "exotic".
	api.EXPECT().GetFlagsPage(gomock.Any(), "proj", int64(100), int64(0)).
		Return([]ldapi.FeatureFlag{{
			Key:        "normal",
			Variations: []ldapi.Variation{{Id: strPtr("normal-0"), Name: strPtr("On"), Value: true}},
		}}, 1, nil)

	store.EXPECT().UpsertAvailableVariationsForFlags(gomock.Any(), "proj", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, byFlagKey map[string][]model.Variation) error {
			if v, ok := byFlagKey["normal"]; ok {
				assert.Equal(t, "normal-0", v[0].Id)
				return nil
			}
			// Reconcile pass: the straggler is flipped from pending to unresolvable.
			assert.Equal(t, "unresolvable-0", byFlagKey["exotic"][0].Id)
			return nil
		}).Times(2)

	model.FillVariationNames(ctx, "proj")
}
