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
)

const ctxKeySdk = ctxKey("adapters.sdk")

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
}

func newSdk(streamingUrl string) Sdk {
	return streamingSdk{
		streamingUrl: streamingUrl,
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
	ldClient, err := ldsdk.MakeCustomClient(sdkKey, config, 5*time.Second)
	if err != nil {
		return flagstate.AllFlags{}, err
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
