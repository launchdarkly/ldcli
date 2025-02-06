package backup_test

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"os"
	"strconv"

	"github.com/launchdarkly/ldcli/internal/dev_server/db/backup"
	"github.com/stretchr/testify/require"
	"testing"
)

type myTable struct {
	key       string
	someValue string
}

func TestDbBackup(t *testing.T) {
	ctx := context.Background()
	dbPath := "test_source_for_backup.db"

	original, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.Remove(dbPath))
	}()

	originalResults := createAndSeedTable(ctx, original, t)
	dataSize := len(originalResults)

	manager := backup.NewManager(dbPath, "main", "ld_cli_backup_test*.bak", "ld_cli_restore_test*.bak")

	for i := 0; i < 5; i++ {
		t.Run("Backup_"+strconv.Itoa(i), func(t *testing.T) {
			result, err := manager.MakeBackupFile(ctx)
			require.NoError(t, err)
			require.NotEqual(t, "", result)

			backupDb, err := sql.Open("sqlite3", result)
			require.NoError(t, err)

			resultsFromBackup, err := getResults(backupDb)
			require.NoError(t, err)
			require.Len(t, resultsFromBackup, dataSize)

			require.Equal(t, originalResults, resultsFromBackup)

			require.NoError(t, backupDb.Close())
		})
	}
}

func TestDbRestore(t *testing.T) {
	ctx := context.Background()
	dbToRestorePath := "test_source_for_restore.db"
	originalDbPathOrigin := "test_source_for_restore_origin.db"

	original, err := sql.Open("sqlite3", dbToRestorePath)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.Remove(dbToRestorePath))
	}()

	originals := createAndSeedTable(ctx, original, t)
	require.NoError(t, original.Close())
	require.NotEmpty(t, originals)

	t.Run("Valid restore completed", func(t *testing.T) {
		manager := backup.NewManager(originalDbPathOrigin, "main", "ld_cli_backup_test*.bak", "ld_cli_restore_test*.bak")
		manager.AddValidationQueries("select count(1) from my_table")

		f, err := os.Open(dbToRestorePath)
		require.NoError(t, err)

		restoredLocation, err := manager.RestoreToFile(ctx, f)
		require.NoError(t, err)
		require.NotEqual(t, "", restoredLocation)
	})

	t.Run("Query validation fails should error", func(t *testing.T) {
		manager := backup.NewManager(originalDbPathOrigin, "main", "ld_cli_backup_test*.bak", "ld_cli_restore_test*.bak")
		manager.AddValidationQueries("select count(1) from a_non_existent_table")

		f, err := os.Open(dbToRestorePath)
		require.NoError(t, err)

		restoredLocation, err := manager.RestoreToFile(ctx, f)
		require.Error(t, err)
		require.Equal(t, "", restoredLocation)
	})

}

const createTable = `CREATE TABLE IF NOT EXISTS my_table (
key text PRIMARY KEY,
some_value text NOT NULL
)`
const insertStatement = `INSERT INTO  my_table (key, some_value) VALUES (?, ?)`

func createAndSeedTable(ctx context.Context, db *sql.DB, t *testing.T) []myTable {
	_, err := db.ExecContext(ctx, createTable)
	require.NoError(t, err)

	dataSize := 50
	for i := 0; i < dataSize; i++ {
		_, err = db.ExecContext(ctx, insertStatement, uuid.New(), uuid.New())
		require.NoError(t, err)
	}

	originalResults, err := getResults(db)
	require.NoError(t, err)
	require.Len(t, originalResults, dataSize)
	return originalResults
}

func getResults(db *sql.DB) ([]myTable, error) {
	rows, err := db.Query("select key, some_value from my_table order by key desc")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []myTable
	for rows.Next() {
		r := myTable{}
		err = rows.Scan(&r.key, &r.someValue)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, nil
}
