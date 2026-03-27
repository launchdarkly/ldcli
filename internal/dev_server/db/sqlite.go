package db

import (
	"context"
	"database/sql"
	"io"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/db/backup"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

type Sqlite struct {
	database *sql.DB
	dbPath   string

	backupManager *backup.Manager
}

var _ model.Store = &Sqlite{}

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

	return tx.Commit()
}
