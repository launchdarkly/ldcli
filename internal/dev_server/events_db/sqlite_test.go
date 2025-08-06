package events_db_test

import (
	"context"
	"os"
	"testing"

	_ "embed"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/dev_server/events_db"
)

//go:embed testevent.json
var testEvent string

func TestDBFunctions(t *testing.T) {
	ctx := context.Background()
	dbName := "events_test.db"
	defer func() {
		require.NoError(t, os.Remove(dbName))
	}()

	store, err := events_db.NewSqlite(ctx, dbName)
	require.NoError(t, err)

	require.NotNil(t, store)

	debugSessionKey := "test"

	t.Run("CreateDebugSession succeeds", func(t *testing.T) {
		err := store.CreateDebugSession(ctx, debugSessionKey)
		require.NoError(t, err)

		// Test that creating the same session again error
		err = store.CreateDebugSession(ctx, debugSessionKey)
		require.Error(t, err)

		// Test creating a different session
		err = store.CreateDebugSession(ctx, "another-session")
		require.NoError(t, err)
	})

	t.Run("WriteEvent succeeds", func(t *testing.T) {
		err := store.WriteEvent(ctx, debugSessionKey, "summary", []byte(testEvent))
		require.NoError(t, err)
	})

	t.Run("QueryEvents with no filter", func(t *testing.T) {
		// Write some test events
		err := store.WriteEvent(ctx, debugSessionKey, "summary", []byte(testEvent))
		require.NoError(t, err)
		err = store.WriteEvent(ctx, debugSessionKey, "diagnostic", []byte(`{"kind":"diagnostic","data":"test"}`))
		require.NoError(t, err)
		err = store.WriteEvent(ctx, debugSessionKey, "summary", []byte(testEvent))
		require.NoError(t, err)

		// Query all events
		page, err := store.QueryEvents(ctx, debugSessionKey, nil, 10, 0)
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Events, 4) // 3 new + 1 from previous test
		require.Equal(t, int64(4), page.TotalCount)
		require.False(t, page.HasMore)
	})

	t.Run("QueryEvents with kind filter", func(t *testing.T) {
		kind := "summary"
		page, err := store.QueryEvents(ctx, debugSessionKey, &kind, 10, 0)
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Events, 3) // Only summary events
		require.Equal(t, int64(3), page.TotalCount)
		require.False(t, page.HasMore)

		// Verify all returned events have the correct kind
		for _, event := range page.Events {
			require.Equal(t, "summary", event.Kind)
		}
	})

	t.Run("QueryEvents with pagination", func(t *testing.T) {
		// Query with limit
		page, err := store.QueryEvents(ctx, debugSessionKey, nil, 2, 0)
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Events, 2)
		require.Equal(t, int64(4), page.TotalCount)
		require.True(t, page.HasMore)

		// Query next page
		page, err = store.QueryEvents(ctx, debugSessionKey, nil, 2, 2)
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Events, 2)
		require.Equal(t, int64(4), page.TotalCount)
		require.False(t, page.HasMore)
	})

}
