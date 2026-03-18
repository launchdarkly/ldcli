package adapters

import (
	"context"
	"log"
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

const (
	sdkClientIdleTimeout     = 5 * time.Minute
	sdkClientCleanupInterval = 1 * time.Minute
)

type sdkClient struct {
	client   *ldsdk.LDClient
	mu       sync.Mutex
	refCount int
	lastUsed time.Time
}

type sdkClientCache struct {
	mu      sync.RWMutex
	clients map[string]*sdkClient
	stopCh  chan struct{}
}

var clientCache = &sdkClientCache{
	clients: make(map[string]*sdkClient),
	stopCh:  make(chan struct{}),
}

func init() {
	go clientCache.cleanupLoop()
}

func (c *sdkClientCache) cleanupLoop() {
	ticker := time.NewTicker(sdkClientCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.cleanupIdleClients()
		case <-c.stopCh:
			c.closeAllClients()
			return
		}
	}
}

func (c *sdkClientCache) cleanupIdleClients() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, sc := range c.clients {
		sc.mu.Lock()
		if sc.refCount == 0 && now.Sub(sc.lastUsed) > sdkClientIdleTimeout {
			if err := sc.client.Close(); err != nil {
				log.Printf("error closing idle SDK client for key %s: %v", key, err)
			}
			delete(c.clients, key)
		}
		sc.mu.Unlock()
	}
}

func (c *sdkClientCache) closeAllClients() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, sc := range c.clients {
		if err := sc.client.Close(); err != nil {
			log.Printf("error closing SDK client for key %s: %v", key, err)
		}
	}
}

func (c *sdkClientCache) get(sdkKey string) (*sdkClient, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sc, ok := c.clients[sdkKey]
	return sc, ok
}

func (c *sdkClientCache) set(sdkKey string, client *sdkClient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[sdkKey] = client
}

func WithSdk(ctx context.Context, s Sdk) context.Context {
	return context.WithValue(ctx, ctxKeySdk, s)
}

func GetSdk(ctx context.Context) Sdk {
	return ctx.Value(ctxKeySdk).(Sdk)
}

//go:generate go run go.uber.org/mock/mockgen -destination mocks/sdk.go -package mocks . Sdk
type Sdk interface {
	GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, error)
	Close() error
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
	sc, ok := clientCache.get(sdkKey)
	if !ok {
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
		sc = &sdkClient{
			client:   ldClient,
			lastUsed: time.Now(),
		}
		clientCache.set(sdkKey, sc)
	}

	sc.mu.Lock()
	sc.refCount++
	sc.lastUsed = time.Now()
	sc.mu.Unlock()

	flags := sc.client.AllFlagsState(ldContext)

	sc.mu.Lock()
	sc.refCount--
	sc.mu.Unlock()

	return flags, nil
}

func (s streamingSdk) Close() error {
	clientCache.closeAllClients()
	close(clientCache.stopCh)
	return nil
}
