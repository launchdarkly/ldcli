package adapters

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
