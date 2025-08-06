package events_db

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	_ "github.com/mattn/go-sqlite3"
)

type Sqlite struct {
	database *sql.DB
	dbPath   string
}

func (s *Sqlite) WriteEvent(ctx context.Context, kind string, data json.RawMessage) error {
	_, err := s.database.ExecContext(ctx, `
		INSERT INTO debug_events (kind, data)
		VALUES (?, ?)`, kind, data)
	return err
}

var _ model.EventStore = &Sqlite{}

func NewSqlite(ctx context.Context, dbPath string) (*Sqlite, error) {
	store := new(Sqlite)
	store.dbPath = dbPath
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
	CREATE TABLE IF NOT EXISTS debug_events (
	    id INTEGER PRIMARY KEY AUTOINCREMENT,
    	written_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		kind text,
		data jsonb NOT NULL
	)`)
	if err != nil {
		return err
	}

	return tx.Commit()
}
