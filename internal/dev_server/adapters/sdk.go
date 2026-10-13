package adapters

import (
	"context"
	"log"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	ldsdk "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
	"github.com/pkg/errors"
)

const ctxKeySdk = ctxKey("adapters.sdk")

// DefaultSdkInitTimeout is how long the SDK client waits for the streaming connection to deliver the
// initial flag payload before giving up. Large projects on slow links may need a longer timeout.
const DefaultSdkInitTimeout = 5 * time.Second

func WithSdk(ctx context.Context, s Sdk) context.Context {
	return context.WithValue(ctx, ctxKeySdk, s)
}

func GetSdk(ctx context.Context) Sdk {
	return ctx.Value(ctxKeySdk).(Sdk)
}

//go:generate go run go.uber.org/mock/mockgen -destination mocks/sdk.go -package mocks . Sdk
type Sdk interface {
	GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error)
}

type streamingSdk struct {
	streamingUrl string
	initTimeout  time.Duration
}

func newSdk(streamingUrl string, initTimeout time.Duration) Sdk {
	if initTimeout <= 0 {
		initTimeout = DefaultSdkInitTimeout
	}
	return streamingSdk{
		streamingUrl: streamingUrl,
		initTimeout:  initTimeout,
	}
}

func (s streamingSdk) GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error) {
	config := ldsdk.Config{
		DiagnosticOptOut: true,
		Events:           ldcomponents.NoEvents(),
		Logging:          ldcomponents.Logging().MinLevel(ldlog.Debug),
	}
	if s.streamingUrl != "" {
		config.ServiceEndpoints.Streaming = s.streamingUrl
	}
	ldClient, err := ldsdk.MakeCustomClient(sdkKey, config, s.initTimeout)
	if err != nil {
		return flagstate.AllFlags{}, errors.Wrap(err, "unable to get source flags from LD SDK")
	}
	defer func() {
		err := ldClient.Close()
		if err != nil {
			log.Printf("error while closing SDK client: %+v", err)
		}
	}()
	flags := ldClient.AllFlagsState(ldContext)
	return flags, nil
}
