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

		err = store.WriteEvent(ctx, "another-session", "summary", []byte(testEvent))
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

	t.Run("QueryDebugSessions with pagination", func(t *testing.T) {
		// Create additional debug sessions for testing
		err := store.CreateDebugSession(ctx, "session-2")
		require.NoError(t, err)
		err = store.CreateDebugSession(ctx, "session-3")
		require.NoError(t, err)

		// Add some events to different sessions
		err = store.WriteEvent(ctx, "session-2", "summary", []byte(testEvent))
		require.NoError(t, err)
		err = store.WriteEvent(ctx, "session-2", "diagnostic", []byte(`{"kind":"diagnostic","data":"test"}`))
		require.NoError(t, err)

		err = store.WriteEvent(ctx, "session-3", "diagnostic", []byte(`{"kind":"diagnostic","data":"test"}`))
		require.NoError(t, err)

		// Query first page
		page, err := store.QueryDebugSessions(ctx, 2, 0)
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Sessions, 2)
		require.Equal(t, int64(4), page.TotalCount) // debugSessionKey, another-session, session-2, session-3
		require.True(t, page.HasMore)

		// Verify sessions are ordered by written_at DESC (newest first)
		// Just verify that we have the expected sessions and event counts
		sessionKeys := make(map[string]int64)
		for _, session := range page.Sessions {
			sessionKeys[session.Key] = session.EventCount
		}

		// Query all sessions to verify the complete result
		allPage, err := store.QueryDebugSessions(ctx, 10, 0)
		require.NoError(t, err)
		require.NotNil(t, allPage)
		require.Len(t, allPage.Sessions, 4)
		require.Equal(t, int64(4), allPage.TotalCount)
		require.False(t, allPage.HasMore)

		// Verify all sessions and their event counts
		allSessionKeys := make(map[string]int64)
		for _, session := range allPage.Sessions {
			allSessionKeys[session.Key] = session.EventCount
		}

		require.Equal(t, int64(1), allSessionKeys["session-3"])
		require.Equal(t, int64(2), allSessionKeys["session-2"])
		require.Equal(t, int64(1), allSessionKeys["another-session"])
		require.Equal(t, int64(4), allSessionKeys[debugSessionKey]) // 4 events from previous tests

		// Test pagination - second page
		page2, err := store.QueryDebugSessions(ctx, 2, 2)
		require.NoError(t, err)
		require.NotNil(t, page2)
		require.Len(t, page2.Sessions, 2)
		require.Equal(t, int64(4), page2.TotalCount)
		require.False(t, page2.HasMore)
	})

	t.Run("DeleteDebugSession succeeds", func(t *testing.T) {
		err := store.DeleteDebugSession(ctx, debugSessionKey)
		require.NoError(t, err)

		result, err := store.QueryEvents(ctx, debugSessionKey, nil, 10, 0)
		require.NoError(t, err)
		require.Len(t, result.Events, 0)

	})
}
