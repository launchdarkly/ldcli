package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/db/backup"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

type Sqlite struct {
	database *sql.DB
	dbPath   string

	backupManager *backup.Manager
}

var _ model.Store = &Sqlite{}

func (s *Sqlite) GetDevProjectKeys(ctx context.Context) ([]string, error) {
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

func (s *Sqlite) GetDevProject(ctx context.Context, key string) (*model.Project, error) {
	var project model.Project
	var contextData string
	var flagStateData string

	row := s.database.QueryRowContext(ctx, `
        SELECT key, source_environment_key, client_side_id, context, last_sync_time, flag_state 
        FROM projects 
        WHERE key = ?
    `, key)

	if err := row.Scan(&project.Key, &project.SourceEnvironmentKey, &project.ClientSideId, &contextData, &project.LastSyncTime, &flagStateData); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.NewErrNotFound("project", key)
		}
		return nil, err
	}

	// Parse the context JSON string
	if err := json.Unmarshal([]byte(contextData), &project.Context); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal context data")
	}

	// Parse the flag state JSON string
	if err := json.Unmarshal([]byte(flagStateData), &project.AllFlagsState); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal flag state data")
	}

	return &project, nil
}

func (s *Sqlite) GetDevProjectByClientSideId(ctx context.Context, clientSideId string) (*model.Project, error) {
	var project model.Project
	var contextData string
	var flagStateData string

	row := s.database.QueryRowContext(ctx, `
        SELECT key, source_environment_key, client_side_id, context, last_sync_time, flag_state 
        FROM projects 
        WHERE client_side_id = ? AND client_side_id IS NOT NULL
    `, clientSideId)

	if err := row.Scan(&project.Key, &project.SourceEnvironmentKey, &project.ClientSideId, &contextData, &project.LastSyncTime, &flagStateData); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.NewErrNotFound("project", clientSideId)
		}
		return nil, err
	}

	// Parse the context JSON string
	if err := json.Unmarshal([]byte(contextData), &project.Context); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal context data")
	}

	// Parse the flag state JSON string
	if err := json.Unmarshal([]byte(flagStateData), &project.AllFlagsState); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal flag state data")
	}

	return &project, nil
}

func (s *Sqlite) UpdateProject(ctx context.Context, project model.Project) (bool, error) {
	flagsStateJson, err := json.Marshal(project.AllFlagsState)
	if err != nil {
		return false, errors.Wrap(err, "unable to marshal flags state when updating project")
	}

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `
		UPDATE projects
		SET flag_state = ?, last_sync_time = ?, context=?, source_environment_key=?, client_side_id=?
		WHERE key = ?;
	`, flagsStateJson, project.LastSyncTime, project.Context.JSONString(), project.SourceEnvironmentKey, project.ClientSideId, project.Key)
	if err != nil {
		return false, errors.Wrap(err, "unable to execute update project")
	}

	// Delete all and add all new variations. Definitely room for optimization...
	_, err = tx.ExecContext(ctx, `
		DELETE FROM available_variations
		WHERE project_key = ?
	`, project.Key)
	if err != nil {
		return false, err
	}

	err = InsertAvailableVariations(ctx, tx, project)
	if err != nil {
		return false, err
	}

	// Delete all overrides that are linked to a flag that is no longer in the project
	// https://github.com/launchdarkly/ldcli/issues/541#issuecomment-2920512092
	_, err = tx.ExecContext(ctx, `
		DELETE FROM overrides
		WHERE project_key = ? AND flag_key NOT IN (SELECT flag_key FROM available_variations WHERE project_key = ?)
	`, project.Key, project.Key)
	if err != nil {
		return false, err
	}

	err = tx.Commit()
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

func (s *Sqlite) DeleteDevProject(ctx context.Context, key string) (bool, error) {
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

func InsertAvailableVariations(ctx context.Context, tx *sql.Tx, project model.Project) (err error) {
	for _, variation := range project.AvailableVariations {
		jsonValue, err := variation.Value.MarshalJSON()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO available_variations
				(project_key, flag_key, id, value, description, name)
			VALUES (?, ?, ?, ?, ?, ?)
		`, project.Key, variation.FlagKey, variation.Id, string(jsonValue), variation.Description, variation.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Sqlite) InsertProject(ctx context.Context, project model.Project) (err error) {
	flagsStateJson, err := json.Marshal(project.AllFlagsState)
	if err != nil {
		return errors.Wrap(err, "unable to marshal flags state when writing project")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	projects, err := tx.QueryContext(ctx, `
SELECT 1 FROM projects WHERE key = ?
`, project.Key)
	if err != nil {
		return
	}
	if projects.Next() {
		err = model.NewErrAlreadyExists("project", project.Key)
		return
	}
	err = projects.Close()
	if err != nil {
		return
	}
	_, err = tx.Exec(`
INSERT INTO projects (key, source_environment_key, client_side_id, context, last_sync_time, flag_state)
VALUES (?, ?, ?, ?, ?, ?)
`,
		project.Key,
		project.SourceEnvironmentKey,
		project.ClientSideId,
		project.Context.JSONString(),
		project.LastSyncTime,
		string(flagsStateJson),
	)
	if err != nil {
		return
	}

	err = InsertAvailableVariations(ctx, tx, project)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Sqlite) GetAvailableVariationsForProject(ctx context.Context, projectKey string) (map[string][]model.Variation, error) {
	rows, err := s.database.QueryContext(ctx, `
			SELECT flag_key, id, name, description, value
			FROM available_variations
			WHERE project_key = ?
		`, projectKey)

	if err != nil {
		return nil, err
	}

	availableVariations := make(map[string][]model.Variation)
	for rows.Next() {
		var flagKey string
		var id string
		var nameNullable sql.NullString
		var descriptionNullable sql.NullString
		var valueJson string

		err = rows.Scan(&flagKey, &id, &nameNullable, &descriptionNullable, &valueJson)
		if err != nil {
			return nil, err
		}

		var value ldvalue.Value
		err = json.Unmarshal([]byte(valueJson), &value)
		if err != nil {
			return nil, err
		}

		var name, description *string
		if nameNullable.Valid {
			name = &nameNullable.String
		}
		if descriptionNullable.Valid {
			description = &descriptionNullable.String
		}
		availableVariations[flagKey] = append(availableVariations[flagKey], model.Variation{
			Id:          id,
			Name:        name,
			Description: description,
			Value:       value,
		})
	}
	return availableVariations, nil
}

func (s *Sqlite) GetOverridesForProject(ctx context.Context, projectKey string) (model.Overrides, error) {
	rows, err := s.database.QueryContext(ctx, `
        SELECT  flag_key, active, value, version
        FROM overrides 
        WHERE project_key = ?
    `, projectKey)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	overrides := make(model.Overrides, 0)
	for rows.Next() {
		var flagKey string
		var active bool
		var value string
		var version int

		err = rows.Scan(&flagKey, &active, &value, &version)
		if err != nil {
			return nil, err
		}

		var ldValue ldvalue.Value
		err = json.Unmarshal([]byte(value), &ldValue)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, model.Override{
			ProjectKey: projectKey,
			FlagKey:    flagKey,
			Value:      ldValue,
			Active:     active,
			Version:    version,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return overrides, nil
}

func (s *Sqlite) UpsertOverride(ctx context.Context, override model.Override) (model.Override, error) {
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

func (s *Sqlite) DeactivateOverride(ctx context.Context, projectKey, flagKey string) (int, error) {
	row := s.database.QueryRowContext(ctx, `
		UPDATE overrides
		set active = false, version = version+1
		where project_key = ? and flag_key = ? and active = true
		returning version
	`,
		projectKey,
		flagKey,
	)
	var version int
	if err := row.Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.Wrapf(model.NewErrNotFound("flag", flagKey), "no override in project %s", projectKey)
		}
		return 0, err
	}

	return version, nil
}

func (s *Sqlite) RestoreBackup(ctx context.Context, stream io.Reader) (string, error) {
	filepath, err := s.backupManager.RestoreToFile(ctx, stream)
	if err != nil {
		return "", errors.Wrap(err, "unable to restore backup db")
	}
	err = s.database.Close()
	if err != nil {
		return "", errors.Wrap(err, "unable to close database before restoring backup")
	}
	err = os.Rename(filepath, s.dbPath)
	if err != nil {
		//panic because this would really leave the app in an invalid state
		panic(err)
	}
	s.database, err = sql.Open("sqlite3", s.dbPath)
	if err != nil {
		//panic because this would really leave the app in an invalid state
		panic(err)
	}

	err = s.runMigrations(ctx)
	if err != nil {
		return "", errors.Wrap(err, "unable to run migrations after restoring backup")
	}

	return filepath, err
}

func (s *Sqlite) CreateBackup(ctx context.Context) (io.ReadCloser, int64, error) {
	backupPath, err := s.backupManager.MakeBackupFile(ctx)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "unable to make backup file, %s", backupPath)
	}
	fi, err := os.Open(backupPath)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "unable to open backup db at %s", backupPath)
	}
	stat, err := fi.Stat()
	if err != nil {
		return nil, 0, errors.Wrapf(err, "unable to stat backup db at %s", backupPath)
	}
	return fi, stat.Size(), nil
}

func NewSqlite(ctx context.Context, dbPath string) (*Sqlite, error) {
	store := new(Sqlite)
	store.dbPath = dbPath
	store.backupManager = backup.NewManager(dbPath, "main", "ld_cli_*.bak", "ld_cli_restore_*.db")
	store.backupManager.AddValidationQueries(validationQueries...)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return &Sqlite{}, err
	}
	store.database = db
	err = store.runMigrations(ctx)
	if err != nil {
		return &Sqlite{}, err
	}
	return store, nil
}

var validationQueries = []string{
	"SELECT COUNT(1) from projects",
	"SELECT COUNT(1) from overrides",
	"SELECT COUNT(1) from available_variations",
}

func isColumnExistsError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "duplicate column name: client_side_id") ||
		strings.Contains(err.Error(), "table projects has no column named client_side_id"))
}

func (s *Sqlite) runMigrations(ctx context.Context) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.Exec(`
	CREATE TABLE IF NOT EXISTS projects (
		key text PRIMARY KEY,
		source_environment_key text NOT NULL,
		client_side_id text,
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

	_, err = tx.Exec(`
	CREATE TABLE IF NOT EXISTS available_variations (
		project_key text NOT NULL,
		flag_key text NOT NULL,
		id text NOT NULL,
		value text NOT NULL, 
		description text,
		name text,
		FOREIGN KEY (project_key) REFERENCES projects (key) ON DELETE CASCADE,
		UNIQUE (project_key, flag_key, id) ON CONFLICT REPLACE
	)`)
	if err != nil {
		return err
	}

	// Add client_side_id column to existing projects table if it doesn't exist
	_, err = tx.Exec(`ALTER TABLE projects ADD COLUMN client_side_id text`)
	if err != nil {
		// Ignore error if column already exists
		if !isColumnExistsError(err) {
			return err
		}
	}

	return tx.Commit()
}
