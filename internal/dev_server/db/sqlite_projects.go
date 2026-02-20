package db

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/pkg/errors"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

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
        SELECT key, source_environment_key, context, last_sync_time, flag_state 
        FROM projects 
        WHERE key = ?
    `, key)

	if err := row.Scan(&project.Key, &project.SourceEnvironmentKey, &contextData, &project.LastSyncTime, &flagStateData); err != nil {
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
		SET flag_state = ?, last_sync_time = ?, context=?, source_environment_key=?
		WHERE key = ?;
	`, flagsStateJson, project.LastSyncTime, project.Context.JSONString(), project.SourceEnvironmentKey, project.Key)
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
INSERT INTO projects (key, source_environment_key, context, last_sync_time, flag_state)
VALUES (?, ?, ?, ?, ?)
`,
		project.Key,
		project.SourceEnvironmentKey,
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
