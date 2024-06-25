package adapters

import (
	"context"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	ldsdk "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
)

const ctxKeySdk = ctxKey("adapters.sdk")

func WithSdk(ctx context.Context, s Sdk) context.Context {
	return context.WithValue(ctx, ctxKeySdk, s)
}

func GetSdk(ctx context.Context) Sdk {
	return ctx.Value(ctxKeySdk).(Sdk)
}

type Sdk struct {
	eventsUrl    string
	pollingUrl   string
	streamingUrl string
}

func newSdk(eventsUrl, pollingUrl, streamingUrl string) Sdk {
	return Sdk{
		eventsUrl:    eventsUrl,
		pollingUrl:   pollingUrl,
		streamingUrl: streamingUrl,
	}
}

func (s Sdk) GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error) {
	config := ldsdk.Config{}
	if s.pollingUrl != "" {
		config.ServiceEndpoints.Polling = s.pollingUrl
	}
	if s.eventsUrl != "" {
		config.ServiceEndpoints.Events = s.eventsUrl
	}
	if s.streamingUrl != "" {
		config.ServiceEndpoints.Streaming = s.streamingUrl
	}
	ldClient, err := ldsdk.MakeCustomClient(sdkKey, config, 5*time.Second)
	if err != nil {
		return flagstate.AllFlags{}, err
	}
	flags := ldClient.AllFlagsState(ldContext)
	return flags, nil
}
