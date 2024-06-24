package adapters

import (
	"context"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	ldsdk "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
)

const ctxKeySdk = "adapters.sdk"

func WithSdk(ctx context.Context, s Sdk) context.Context {
	return context.WithValue(ctx, ctxKeySdk, s)
}

func GetSdk(ctx context.Context) Sdk {
	return ctx.Value(ctxKeySdk).(Sdk)
}

type Sdk struct {
}

func newSdk() Sdk {
	return Sdk{}
}

func (s Sdk) GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error) {
	ldClient, err := ldsdk.MakeClient(sdkKey, 5*time.Second)
	if err != nil {
		return flagstate.AllFlags{}, nil
	}
	flags := ldClient.AllFlagsState(ldContext)
	return flags, nil
}
