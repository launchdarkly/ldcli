package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReorderSDKs(t *testing.T) {
	makeSDKs := func(ids ...string) []sdkDetail {
		details := make([]sdkDetail, len(ids))
		for i, id := range ids {
			details[i] = sdkDetail{id: id, index: i}
		}
		return details
	}

	t.Run("no detected IDs preserves original order", func(t *testing.T) {
		all := makeSDKs("go-server-sdk", "python-server-sdk", "node-server")

		result := reorderSDKs(all, nil)

		require.Len(t, result, 3)
		assert.Equal(t, "go-server-sdk", result[0].id)
		assert.Equal(t, "python-server-sdk", result[1].id)
		assert.Equal(t, "node-server", result[2].id)
	})

	t.Run("detected SDK is moved to front", func(t *testing.T) {
		all := makeSDKs("go-server-sdk", "python-server-sdk", "node-server")

		result := reorderSDKs(all, []string{"python-server-sdk"})

		require.Len(t, result, 3)
		assert.Equal(t, "python-server-sdk", result[0].id)
		assert.Equal(t, "go-server-sdk", result[1].id)
		assert.Equal(t, "node-server", result[2].id)
	})

	t.Run("multiple detected SDKs are placed in detection order", func(t *testing.T) {
		all := makeSDKs("go-server-sdk", "python-server-sdk", "node-server", "react-client-sdk")

		result := reorderSDKs(all, []string{"node-server", "go-server-sdk"})

		require.Len(t, result, 4)
		assert.Equal(t, "node-server", result[0].id)
		assert.Equal(t, "go-server-sdk", result[1].id)
		// remaining are in original popularity order
		assert.Equal(t, "python-server-sdk", result[2].id)
		assert.Equal(t, "react-client-sdk", result[3].id)
	})

	t.Run("detected ID not in all list is silently ignored", func(t *testing.T) {
		all := makeSDKs("go-server-sdk", "python-server-sdk")

		result := reorderSDKs(all, []string{"nonexistent-sdk"})

		require.Len(t, result, 2)
		assert.Equal(t, "go-server-sdk", result[0].id)
		assert.Equal(t, "python-server-sdk", result[1].id)
	})

	t.Run("indices are reassigned sequentially after reorder", func(t *testing.T) {
		all := makeSDKs("go-server-sdk", "python-server-sdk", "node-server")

		result := reorderSDKs(all, []string{"node-server"})

		for i, s := range result {
			assert.Equal(t, i, s.index, "index at position %d should be %d, got %d", i, i, s.index)
		}
	})

	t.Run("all SDKs detected preserves detection order", func(t *testing.T) {
		all := makeSDKs("go-server-sdk", "python-server-sdk", "node-server")
		detected := []string{"node-server", "go-server-sdk", "python-server-sdk"}

		result := reorderSDKs(all, detected)

		require.Len(t, result, 3)
		assert.Equal(t, "node-server", result[0].id)
		assert.Equal(t, "go-server-sdk", result[1].id)
		assert.Equal(t, "python-server-sdk", result[2].id)
	})

	t.Run("empty all list returns empty result", func(t *testing.T) {
		result := reorderSDKs([]sdkDetail{}, []string{"go-server-sdk"})

		assert.Empty(t, result)
	})
}

func TestInitSDKs(t *testing.T) {
	sdks := initSDKs()

	t.Run("returns non-empty list", func(t *testing.T) {
		assert.NotEmpty(t, sdks)
	})

	t.Run("all items have non-empty IDs and display names", func(t *testing.T) {
		for _, s := range sdks {
			assert.NotEmpty(t, s.id, "id should not be empty")
			assert.NotEmpty(t, s.displayName, "displayName should not be empty for %s", s.id)
		}
	})

	t.Run("indices are sequential starting at 0", func(t *testing.T) {
		for i, s := range sdks {
			assert.Equal(t, i, s.index, "expected index %d for SDK %s, got %d", i, s.id, s.index)
		}
	})

	t.Run("contains expected SDKs", func(t *testing.T) {
		ids := make(map[string]bool)
		for _, s := range sdks {
			ids[s.id] = true
		}
		for _, expected := range []string{"go-server-sdk", "python-server-sdk", "node-server", "react-client-sdk", "java-server-sdk"} {
			assert.True(t, ids[expected], "expected SDK %s to be in list", expected)
		}
	})
}
