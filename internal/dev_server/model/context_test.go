package model_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func TestStoreFromContext(t *testing.T) {
	t.Run("returns the store when present in the context", func(t *testing.T) {
		store := mocks.NewMockStore(nil)
		ctx := model.ContextWithStore(context.Background(), store)

		assert.Equal(t, store, model.StoreFromContext(ctx))
	})

	t.Run("returns nil when the store is missing from the context", func(t *testing.T) {
		require.NotPanics(t, func() {
			assert.Nil(t, model.StoreFromContext(context.Background()))
		})
	})
}

func TestEventStoreFromContext(t *testing.T) {
	t.Run("returns the event store when present in the context", func(t *testing.T) {
		store := mocks.NewMockEventStore(nil)
		ctx := model.ContextWithEventStore(context.Background(), store)

		assert.Equal(t, store, model.EventStoreFromContext(ctx))
	})

	t.Run("returns nil when the event store is missing from the context", func(t *testing.T) {
		require.NotPanics(t, func() {
			assert.Nil(t, model.EventStoreFromContext(context.Background()))
		})
	})
}
