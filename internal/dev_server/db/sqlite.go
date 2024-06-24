package db

import (
	"context"
	"database/sql"
)
import _ "github.com/mattn/go-sqlite3"

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
