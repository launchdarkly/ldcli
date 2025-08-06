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

	t.Run("WriteEvent succeeds", func(t *testing.T) {
		err := store.WriteEvent(ctx, "summary", []byte(testEvent))
		require.NoError(t, err)
	})

}
