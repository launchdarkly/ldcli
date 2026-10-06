package model

import (
	"context"
	"log"
	"time"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
)

const (
	fillRetries    = 3
	fillRetryDelay = 500 * time.Millisecond
)

// FillVariationsAsync resolves a project's variations from REST in the background. It is a var so tests can stub it.
var FillVariationsAsync = func(ctx context.Context, projectKey string) {
	go func() {
		// Contain panics so a background failure can't take down the process.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("variation fill: recovered from panic for %q: %v", projectKey, r)
			}
		}()
		FillVariations(context.WithoutCancel(ctx), projectKey)
	}()
}

// FillVariations fetches the project's flags from REST and replaces the stored variations with the resolved values and names. It replaces wholesale, so overlapping runs are safe and it needs no locking.
func FillVariations(ctx context.Context, projectKey string) {
	api := adapters.GetApi(ctx)
	var flags []ldapi.FeatureFlag
	var err error
	for attempt := 0; ; attempt++ {
		if flags, err = api.GetAllFlags(ctx, projectKey); err == nil {
			break
		}
		if attempt >= fillRetries {
			log.Printf("variation fill: fetch failed for %q: %v", projectKey, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(fillRetryDelay << attempt):
		}
	}

	if err := StoreFromContext(ctx).SetAvailableVariationsForProject(ctx, projectKey, variationsFromFlags(flags)); err != nil {
		log.Printf("variation fill: store failed for %q: %v", projectKey, err)
	}
}
