package db

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/pkg/errors"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

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
