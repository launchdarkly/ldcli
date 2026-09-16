package adapters

import (
	"context"
	"time"

	ldapi "github.com/launchdarkly/api-client-go/v14"
)

func WithApiAndSdk(ctx context.Context, client ldapi.APIClient, streamingUrl string, sdkInitTimeout time.Duration) context.Context {
	ctx = WithSdk(ctx, newSdk(streamingUrl, sdkInitTimeout))
	ctx = WithApi(ctx, NewApi(client))
	return ctx
}
