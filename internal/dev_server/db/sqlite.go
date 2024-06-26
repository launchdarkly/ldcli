package db

import (
	"context"
	"database/sql"
	"encoding/json"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

var ErrNotFound = errors.New("not found")

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
	if err := json.Unmarshal([]byte(flagStateData), &project.FlagState); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal flag state data")
	}

	return &project, nil
}

func (s Sqlite) DeleteDevProject(ctx context.Context, key string) (bool, error) {
	result, err := s.database.Exec("DELETE FROM projects where key=?", key)
	if err != nil {
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if rowsAffected == 0 {
		return false, nil
	}
	return true, nil
}

func (s Sqlite) InsertProject(ctx context.Context, project model.Project) error {
	flagsStateJson, err := json.Marshal(project.FlagState)
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

func (s Sqlite) UpsertOverride(ctx context.Context, override model.Override) (model.Override, error) {
	valueJson, err := override.Value.MarshalJSON()
	if err != nil {
		return model.Override{}, errors.Wrap(err, "unable to marshal override value when writing override")
	}
	row := s.database.QueryRowContext(ctx, `
		INSERT INTO overrides (project_key, flag_key, value, active)
		VALUES (?, ?, ?, ?)
			ON CONFLICT(flag_key, project_key) DO UPDATE SET
			    value=excluded.value,
			    active=excluded.active,
			    version=version+1
		RETURNING project_key, flag_key, active, value, version;
	`,
		override.ProjectKey,
		override.FlagKey,
		valueJson,
		override.Active,
	)
	var tempValue []byte
	if err := row.Scan(&override.ProjectKey, &override.FlagKey, &override.Active, &tempValue, &override.Version); err != nil {
		return model.Override{}, errors.Wrap(err, "unable to upsert override")
	}
	if err := json.Unmarshal(tempValue, &override.Value); err != nil {
		return model.Override{}, errors.Wrap(err, "unable to unmarshal override value")
	}
	return override, nil
}

func (s Sqlite) DeleteOverride(ctx context.Context, projectKey, flagKey string) error {
	result, err := s.database.Exec(`
		UPDATE overrides set active = false, version = version+1 where project_key = ? and flag_key = ? and active = true
	`,
		projectKey,
		flagKey,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
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

	_, err = tx.Exec(`
	CREATE TABLE IF NOT EXISTS overrides (
		project_key text NOT NULL,
		flag_key text NOT NULL,
		value text NOT NULL,
		active boolean NOT NULL default TRUE,
		version integer NOT NULL default 1,
		UNIQUE (project_key, flag_key) ON CONFLICT REPLACE
	)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
