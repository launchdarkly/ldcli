package adapters

import (
	"context"
	"sync"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	ldsdk "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
	"github.com/pkg/errors"
)

const ctxKeySdk = ctxKey("adapters.sdk")

type streamingSdk struct {
	mu           sync.RWMutex
	clients      map[string]*ldsdk.LDClient
	lastUsed     map[string]time.Time
	streamingUrl string
}

func newSdk(streamingUrl string) Sdk {
	return &streamingSdk{
		clients:      make(map[string]*ldsdk.LDClient),
		lastUsed:     make(map[string]time.Time),
		streamingUrl: streamingUrl,
	}
}

func (s *streamingSdk) GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error) {
	s.mu.RLock()
	client, exists := s.clients[sdkKey]
	s.mu.RUnlock()

	if !exists {
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
			return flagstate.AllFlags{}, errors.Wrap(err, "unable to get source flags from LD SDK")
		}

		s.mu.Lock()
		s.clients[sdkKey] = ldClient
		s.lastUsed[sdkKey] = time.Now()
		s.mu.Unlock()

		client = ldClient
	} else {
		s.mu.Lock()
		s.lastUsed[sdkKey] = time.Now()
		s.mu.Unlock()
	}

	return client.AllFlagsState(ldContext), nil
}

func (s *streamingSdk) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, client := range s.clients {
		client.Close()
		delete(s.clients, key)
	}
	return nil
}

func WithSdk(ctx context.Context, sdk Sdk) context.Context {
	return context.WithValue(ctx, ctxKeySdk, sdk)
}

func GetSdk(ctx context.Context) Sdk {
	if sdk := ctx.Value(ctxKeySdk); sdk != nil {
		return sdk.(Sdk)
	}
	return nil
}

//go:generate go run go.uber.org/mock/mockgen -destination mocks/sdk.go -package mocks . Sdk
type Sdk interface {
	GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error)
	Close() error
}
