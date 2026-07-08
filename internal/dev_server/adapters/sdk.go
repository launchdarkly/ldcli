package adapters

import (
	"context"
	"log"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	ldsdk "github.com/launchdarkly/go-server-sdk/v7"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
	"github.com/pkg/errors"

	"github.com/launchdarkly/go-server-sdk-evaluation/v3/ldmodel"
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
	// GetAllFlagsState connects to the source environment's streaming
	// endpoint and returns both the evaluated flag state for ldContext, and
	// the raw list of variation values for every flag (keyed by flag key) —
	// both come off the same single connection, at no extra cost. The raw
	// values do not include a variation's display name/description; those
	// only exist in the REST API's representation of a flag, never in the
	// flag-delivery/streaming wire format.
	GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, map[string][]ldvalue.Value, error)
}

type streamingSdk struct {
	streamingUrl string
}

func newSdk(streamingUrl string) Sdk {
	return streamingSdk{
		streamingUrl: streamingUrl,
	}
}

// variationCapturingStore wraps the SDK's real in-memory DataStore and only
// overrides Init, to snapshot the raw flag data (including every variation
// value, not just the one resolved for a given context) the moment it
// arrives over the streaming connection. Everything else is delegated
// unchanged to the real store via embedding.
type variationCapturingStore struct {
	subsystems.DataStore
	variationsByFlagKey map[string][]ldvalue.Value
}

func (s *variationCapturingStore) Init(allData []ldstoretypes.Collection) error {
	for _, collection := range allData {
		if collection.Kind != ldstoreimpl.Features() {
			continue
		}
		for _, item := range collection.Items {
			if item.Item.Item == nil {
				continue
			}
			flag, ok := item.Item.Item.(*ldmodel.FeatureFlag)
			if !ok {
				continue
			}
			s.variationsByFlagKey[item.Key] = flag.Variations
		}
	}
	return s.DataStore.Init(allData)
}

type variationCapturingStoreConfigurer struct {
	store *variationCapturingStore
}

func (c variationCapturingStoreConfigurer) Build(clientContext subsystems.ClientContext) (subsystems.DataStore, error) {
	real, err := ldcomponents.InMemoryDataStore().Build(clientContext)
	if err != nil {
		return nil, err
	}
	c.store.DataStore = real
	return c.store, nil
}

func (s streamingSdk) GetAllFlagsState(ctx context.Context, ldContext ldcontext.Context, sdkKey string) (flagstate.AllFlags, map[string][]ldvalue.Value, error) {
	capturingStore := &variationCapturingStore{variationsByFlagKey: make(map[string][]ldvalue.Value)}
	config := ldsdk.Config{
		DiagnosticOptOut: true,
		Events:           ldcomponents.NoEvents(),
		Logging:          ldcomponents.Logging().MinLevel(ldlog.Debug),
		DataStore:        variationCapturingStoreConfigurer{store: capturingStore},
	}
	if s.streamingUrl != "" {
		config.ServiceEndpoints.Streaming = s.streamingUrl
	}
	ldClient, err := ldsdk.MakeCustomClient(sdkKey, config, 5*time.Second)
	if err != nil {
		return flagstate.AllFlags{}, nil, errors.Wrap(err, "unable to get source flags from LD SDK")
	}
	defer func() {
		err := ldClient.Close()
		if err != nil {
			log.Printf("error while closing SDK client: %+v", err)
		}
	}()
	flags := ldClient.AllFlagsState(ldContext)
	return flags, capturingStore.variationsByFlagKey, nil
}
