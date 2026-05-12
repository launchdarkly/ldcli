package rollouts_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/rollouts"
)

// TestClientStubReturnsEmptyEnvelopeShape is the Phase 1 sanity test. It verifies the
// RolloutsClient.List stub returns the expected envelope shape: a non-nil *RolloutList with an
// empty Items slice. Plan 02 replaces this with httptest.NewServer round-trip tests.
func TestClientStubReturnsEmptyEnvelopeShape(t *testing.T) {
	t.Run("List returns non-nil empty RolloutList", func(t *testing.T) {
		c := rollouts.NewClient("test")

		list, err := c.List(
			context.Background(),
			"tok",
			"https://example.test",
			"proj-key",
			"flag-key",
			rollouts.ListOpts{},
		)

		require.NoError(t, err)
		require.NotNil(t, list)
		assert.Equal(t, 0, len(list.Items))
	})

	t.Run("Get returns non-nil zero-value Rollout", func(t *testing.T) {
		c := rollouts.NewClient("test")

		r, err := c.Get(
			context.Background(),
			"tok",
			"https://example.test",
			"proj-key",
			"env-key",
			"rollout-id",
		)

		require.NoError(t, err)
		require.NotNil(t, r)
	})

	t.Run("NewListEnvelope yields the documented v1beta1 shape", func(t *testing.T) {
		list := &rollouts.RolloutList{Items: []rollouts.Rollout{}}
		env := rollouts.NewListEnvelope(list)

		assert.Equal(t, rollouts.SchemaVersionV1Beta1, env.SchemaVersion)
		assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
		assert.Equal(t, "RolloutList", env.Kind)
		assert.NotNil(t, env.Data)
		require.NotNil(t, env.Meta)
		assert.False(t, env.Meta.FetchedAt.IsZero(), "envelope meta.fetchedAt should be set")
	})

	t.Run("NewErrorEnvelope yields the documented error shape", func(t *testing.T) {
		env := rollouts.NewErrorEnvelope("unknown_upstream", "something broke", "try again later")

		assert.Equal(t, "rollouts.v1beta1", env.SchemaVersion)
		assert.Equal(t, "Error", env.Kind)
		require.NotNil(t, env.Error)
		assert.Equal(t, "unknown_upstream", env.Error.Code)
		assert.Equal(t, "something broke", env.Error.Message)
		assert.Equal(t, "try again later", env.Error.NextAction)
	})

	t.Run("compile-time assertions hold", func(t *testing.T) {
		// var _ Client = RolloutsClient{} and var _ Client = &MockClient{} live in source.
		// This subtest is a sentinel — if either assertion is removed the build breaks before
		// reaching here. We still touch the types so the test isn't optimized away.
		var c rollouts.Client = rollouts.NewClient("test")
		_ = c
		var mc rollouts.Client = &rollouts.MockClient{}
		_ = mc
	})
}
