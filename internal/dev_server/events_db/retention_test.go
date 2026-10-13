package events_db

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteEventRetention(t *testing.T) {
	ctx := context.Background()
	dbName := "events_retention_test.db"
	defer func() {
		require.NoError(t, os.Remove(dbName))
	}()

	store, err := NewSqlite(ctx, dbName)
	require.NoError(t, err)
	store.maxEvents = 3
	store.maxEventBytes = maxDebugEventBytes
	require.NoError(t, store.CreateDebugSession(ctx, "session"))

	for i := 0; i < 5; i++ {
		require.NoError(t, store.WriteEvent(ctx, "session", "custom", json.RawMessage(`{}`)))
	}

	page, err := store.QueryEvents(ctx, "session", nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, page.Events, 3)
	require.EqualValues(t, 3, page.TotalCount)
}

func TestWriteEventByteRetention(t *testing.T) {
	ctx := context.Background()
	dbName := "events_byte_retention_test.db"
	defer func() {
		require.NoError(t, os.Remove(dbName))
	}()

	store, err := NewSqlite(ctx, dbName)
	require.NoError(t, err)
	event := json.RawMessage(`{"kind":"custom"}`)
	store.maxEventBytes = int64(2*len(event) + 1)
	require.NoError(t, store.CreateDebugSession(ctx, "session"))

	for i := 0; i < 5; i++ {
		require.NoError(t, store.WriteEvent(ctx, "session", "custom", event))
	}

	page, err := store.QueryEvents(ctx, "session", nil, 10, 0)
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	require.EqualValues(t, 2, page.TotalCount)
}
