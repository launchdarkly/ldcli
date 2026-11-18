package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSdk(t *testing.T) {
	t.Run("returns the sdk when present in the context", func(t *testing.T) {
		s := newSdk("", 0)
		ctx := WithSdk(context.Background(), s)

		assert.Equal(t, s, GetSdk(ctx))
	})

	t.Run("returns nil when the sdk is missing from the context", func(t *testing.T) {
		require.NotPanics(t, func() {
			assert.Nil(t, GetSdk(context.Background()))
		})
	})
}

func TestNewSdkInitTimeout(t *testing.T) {
	t.Run("uses the provided timeout", func(t *testing.T) {
		s, ok := newSdk("", 15*time.Second).(streamingSdk)
		require.True(t, ok)
		assert.Equal(t, 15*time.Second, s.initTimeout)
	})

	t.Run("falls back to the default when zero", func(t *testing.T) {
		s, ok := newSdk("", 0).(streamingSdk)
		require.True(t, ok)
		assert.Equal(t, DefaultSdkInitTimeout, s.initTimeout)
	})

	t.Run("falls back to the default when negative", func(t *testing.T) {
		s, ok := newSdk("", -time.Second).(streamingSdk)
		require.True(t, ok)
		assert.Equal(t, DefaultSdkInitTimeout, s.initTimeout)
	})
}
