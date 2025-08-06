package events_db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	_ "github.com/mattn/go-sqlite3"
)

type Sqlite struct {
	database *sql.DB
	dbPath   string
}

func (s *Sqlite) CreateDebugSession(ctx context.Context, debugSessionKey string) error {
	_, err := s.database.ExecContext(ctx, `
		INSERT INTO debug_session (key)
		VALUES (?)`, debugSessionKey)
	return err
}

func (s *Sqlite) WriteEvent(ctx context.Context, debugSessionKey string, kind string, data json.RawMessage) error {
	_, err := s.database.ExecContext(ctx, `
		INSERT INTO debug_events (kind, debug_session_key, data)
		VALUES (?,?, ?)`, kind, debugSessionKey, data)
	return err
}

func (s *Sqlite) QueryEvents(ctx context.Context, debugSessionKey string, kind *string, limit int, offset int) (*model.EventsPage, error) {
	// Build the query based on whether kind filter is provided
	var query string
	var args []interface{}

	if kind != nil {
		query = `
			SELECT id, written_at, kind, data
			FROM debug_events
			WHERE 
			    debug_session_key = ?
			    AND kind = ?
			ORDER BY id DESC
			LIMIT ? OFFSET ?`
		args = []interface{}{debugSessionKey, *kind, limit, offset}
	} else {
		query = `
			SELECT id, written_at, kind, data
			FROM debug_events
			where debug_session_key = ?
			ORDER BY id DESC
			LIMIT ? OFFSET ?`
		args = []interface{}{debugSessionKey, limit, offset}
	}

	// Execute the main query
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var event model.Event
		var writtenAtStr string

		err := rows.Scan(&event.ID, &writtenAtStr, &event.Kind, &event.Data)
		if err != nil {
			return nil, err
		}

		// Parse the timestamp - SQLite returns ISO 8601 format
		event.WrittenAt, err = time.Parse(time.RFC3339, writtenAtStr)
		if err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Get total count for pagination info
	var totalCount int64
	var countQuery string
	var countArgs []interface{}

	if kind != nil {
		countQuery = `SELECT COUNT(*) FROM debug_events WHERE debug_session_key = ? AND kind = ?`
		countArgs = []interface{}{debugSessionKey, *kind}
	} else {
		countQuery = `SELECT COUNT(*) FROM debug_events WHERE debug_session_key = ?`
		countArgs = []interface{}{debugSessionKey}
	}

	err = s.database.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, err
	}

	// Determine if there are more results
	hasMore := int64(offset+len(events)) < totalCount

	return &model.EventsPage{
		Events:     events,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

func (s *Sqlite) QueryDebugSessions(ctx context.Context, limit int, offset int) (*model.DebugSessionsPage, error) {
	// Execute the main query based on the provided SQL
	query := `
		SELECT debug_session.key, debug_session.written_at, COUNT(debug_events.id) as event_count
		FROM debug_session
		LEFT JOIN debug_events ON debug_session.key = debug_events.debug_session_key
		GROUP BY debug_session.key, debug_session.written_at
		HAVING event_count > 0
		ORDER BY debug_session.written_at DESC
		LIMIT ? OFFSET ?`

	rows, err := s.database.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []model.DebugSession
	for rows.Next() {
		var session model.DebugSession
		var writtenAtStr string

		err := rows.Scan(&session.Key, &writtenAtStr, &session.EventCount)
		if err != nil {
			return nil, err
		}

		// Parse the timestamp - SQLite returns ISO 8601 format
		session.WrittenAt, err = time.Parse(time.RFC3339, writtenAtStr)
		if err != nil {
			return nil, err
		}

		sessions = append(sessions, session)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Get total count for pagination info
	var totalCount int64
	countQuery := `SELECT COUNT(*) FROM debug_session`
	err = s.database.QueryRowContext(ctx, countQuery).Scan(&totalCount)
	if err != nil {
		return nil, err
	}

	// Determine if there are more results
	hasMore := int64(offset+len(sessions)) < totalCount

	return &model.DebugSessionsPage{
		Sessions:   sessions,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

func (s *Sqlite) DeleteDebugSession(ctx context.Context, debugSessionKey string) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM debug_session WHERE key = ?`, debugSessionKey)
	return err
}

func (s *Sqlite) deleteOrphanedEvents(ctx context.Context) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM debug_session WHERE NOT EXISTS (SELECT 1 from debug_events WHERE debug_events.debug_session_key = debug_session.key);`)
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
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		return &Sqlite{}, err
	}
	err = store.runMigrations(ctx)
	if err != nil {
		return &Sqlite{}, err
	}
	err = store.deleteOrphanedEvents(ctx)
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
	CREATE TABLE IF NOT EXISTS debug_session (
		key text PRIMARY KEY,
    	written_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	_, err = tx.Exec(`
	CREATE TABLE IF NOT EXISTS debug_events (
	  	id INTEGER PRIMARY KEY AUTOINCREMENT,
		written_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		kind text,
		data jsonb NOT NULL,
		debug_session_key TEXT NOT NULL,
		FOREIGN KEY (debug_session_key) REFERENCES debug_session (key) ON DELETE CASCADE
	)`)
	if err != nil {
		return err
	}

	return tx.Commit()
}
