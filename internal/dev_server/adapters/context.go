package adapters

import (
	"context"
	ldapi "github.com/launchdarkly/api-client-go/v14"
)

func WithLdApi(ctx context.Context, client ldapi.APIClient, streamingUrl string) context.Context {
	ctx = WithSdk(ctx, newSdk(streamingUrl))
	ctx = WithApi(ctx, NewApi(client))
	return ctx
}
