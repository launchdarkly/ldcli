package db

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
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

func (s Sqlite) UpsertOverride(ctx context.Context, override model.Override) error {
	valueJson, err := override.Value.MarshalJSON()
	if err != nil {
		return errors.Wrap(err, "unable to marshal override value when writing override")
	}

	_, err = s.database.Exec(`
		INSERT INTO overrides (project_key, flag_key, value, active)
		VALUES (?, ?, ?, ?)
			ON CONFLICT(flag_key, project_key) DO UPDATE SET
			    value=excluded.value,
			    active=excluded.active,
			    version=version+1;
	`,
		override.ProjectKey,
		override.FlagKey,
		valueJson,
		override.Active,
	)

	return err
}

func (s Sqlite) DeleteOverride(ctx context.Context, projectKey, flagKey string) error {
	_, err := s.database.Exec(`
		UPDATE overrides set active = false where project_key = ? and flag_key = ?
	`,
		projectKey,
		flagKey,
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
