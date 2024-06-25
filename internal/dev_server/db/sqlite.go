package db

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"

	_ "github.com/mattn/go-sqlite3"
)

type Sqlite struct {
	database *sql.DB
}

func (s Sqlite) GetDevProjects(ctx context.Context) ([]string, error) {
	rows, err := s.database.Query("select key from projects")
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		err = rows.Scan(&key)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (s Sqlite) GetDevProject(ctx context.Context, key string) (*model.Project, error) {
	var project model.Project
	var contextData string
	var flagStateData string

	row := s.database.QueryRowContext(ctx, `
        SELECT key, source_environment_key, context, last_sync_time, flag_state 
        FROM projects 
        WHERE key = ?
    `, key)

	if err := row.Scan(&project.Key, &project.SourceEnvironmentKey, &contextData, &project.LastSyncTime, &flagStateData); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No project found with the given key
		}
		return nil, err
	}

	// Parse the context JSON string
	if err := json.Unmarshal([]byte(contextData), &project.Context); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal context data")
	}

	// Parse the flag state JSON string
	var err error
	project.FlagState, err = UnmarshalFlagStateJSON([]byte(flagStateData))
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal flag state data")
	}

	return &project, nil
}

func (s Sqlite) InsertProject(ctx context.Context, project model.Project) error {
	flagsStateJson, err := project.FlagState.MarshalJSON()
	if err != nil {
		return errors.Wrap(err, "unable to marshal flags state when writing project")
	}
	_, err = s.database.Exec(`
INSERT INTO projects (key, source_environment_key, context, last_sync_time, flag_state)
VALUES (?, ?, ?, ?, ?)
`,
		project.Key,
		project.SourceEnvironmentKey,
		project.Context.JSONString(),
		project.LastSyncTime,
		string(flagsStateJson),
	)
	return err
}

func NewSqlite(ctx context.Context, dbPath string) (Sqlite, error) {
	store := new(Sqlite)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return Sqlite{}, err
	}
	store.database = db
	err = store.runMigrations(ctx)
	if err != nil {
		return Sqlite{}, err
	}
	return *store, nil
}

func (s Sqlite) runMigrations(ctx context.Context) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
	CREATE TABLE IF NOT EXISTS projects (
		key text PRIMARY KEY,
		source_environment_key text NOT NULL,
		context text NOT NULL,
		last_sync_time timestamp NOT NULL,
		flag_state TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
