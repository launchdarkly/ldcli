package model

import (
	"context"
	"log"
	"strings"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"golang.org/x/sync/errgroup"
)

const (
	fillPageSize       = 100
	maxConcurrentPages = 6
)

// FillVariationNamesAsync is a var so tests can stub out the background work.
var FillVariationNamesAsync = func(ctx context.Context, projectKey string) {
	go FillVariationNames(context.WithoutCancel(ctx), projectKey)
}

// FillVariationNames fills in variation names from REST, a page at a time, for
// the streaming values that arrive nameless. Callers run it detached from the sync path.
func FillVariationNames(ctx context.Context, projectKey string) {
	api := adapters.GetApi(ctx)
	store := StoreFromContext(ctx)

	// Skip the fan-out when a prior fill already resolved everything.
	if existing, err := store.GetAvailableVariationsForProject(ctx, projectKey); err == nil && !hasPendingVariations(existing) {
		return
	}

	first, total, err := api.GetFlagsPage(ctx, projectKey, fillPageSize, 0)
	if err != nil {
		log.Printf("variation name fill: initial page failed for %q: %v", projectKey, err)
		return
	}
	if err := upsertPageNames(ctx, store, projectKey, first); err != nil {
		log.Printf("variation name fill: upsert page 0 failed for %q: %v", projectKey, err)
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentPages)
	for offset := int64(fillPageSize); offset < int64(total); offset += fillPageSize {
		offset := offset
		g.Go(func() error {
			page, _, err := api.GetFlagsPage(ctx, projectKey, fillPageSize, offset)
			if err != nil {
				log.Printf("variation name fill: page at offset %d failed for %q: %v", offset, projectKey, err)
				return nil
			}
			if err := upsertPageNames(ctx, store, projectKey, page); err != nil {
				log.Printf("variation name fill: upsert at offset %d failed for %q: %v", offset, projectKey, err)
			}
			return nil
		})
	}
	_ = g.Wait()
}

func upsertPageNames(ctx context.Context, store Store, projectKey string, flags []ldapi.FeatureFlag) error {
	byFlagKey := make(map[string][]Variation, len(flags))
	for _, flag := range flags {
		byFlagKey[flag.Key] = variationsFromFlag(flag)
	}
	if len(byFlagKey) == 0 {
		return nil
	}
	return store.UpsertAvailableVariationsForFlags(ctx, projectKey, byFlagKey)
}

func hasPendingVariations(existing map[string][]Variation) bool {
	for _, variations := range existing {
		for _, v := range variations {
			if strings.HasPrefix(v.Id, pendingIDPrefix) {
				return true
			}
		}
	}
	return false
}

func variationsFromFlag(flag ldapi.FeatureFlag) []Variation {
	variations := make([]Variation, 0, len(flag.Variations))
	for _, v := range flag.Variations {
		variations = append(variations, Variation{
			Id:          *v.Id,
			Description: v.Description,
			Name:        v.Name,
			Value:       ldvalue.CopyArbitraryValue(v.Value),
		})
	}
	return variations
}
