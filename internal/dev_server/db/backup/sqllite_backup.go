package backup

import (
	"context"
	"database/sql"
	"fmt"
	sqllite "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

var c atomic.Int32

type Manager struct {
	dbPath            string
	dbName            string
	backupFilePattern string
	driverName        string
	mutex             sync.Mutex
	conns             []*sqllite.SQLiteConn
}

func NewManager(dbPath string, dbName string, backupFilePattern string) *Manager {
	count := c.Add(1)
	m := &Manager{
		dbPath:            dbPath,
		dbName:            dbName,
		backupFilePattern: backupFilePattern,
		driverName:        fmt.Sprintf("sqlite3-backups-%d", count),
		conns:             make([]*sqllite.SQLiteConn, 0),
		mutex:             sync.Mutex{},
	}
	sql.Register(m.driverName, &sqllite.SQLiteDriver{
		ConnectHook: func(conn *sqllite.SQLiteConn) error {
			m.conns = append(m.conns, conn)
			return nil
		},
	})
	return m
}

func (m *Manager) resetConnections() {
	m.conns = make([]*sqllite.SQLiteConn, 0)
}

// connectToDb opens a sqlite connection and pings the database to populate the underlying sqlite connection
func (m *Manager) connectToDb(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open(m.driverName, path)
	if err != nil {
		return nil, errors.Wrap(err, "open database")
	}

	err = db.PingContext(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "connecting to database database")
	}

	return db, nil
}

// MakeBackupFile returns a string path of the sqlite database backup
func (m *Manager) MakeBackupFile(ctx context.Context) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// clear out any connections from previous backups
	m.resetConnections()

	// Make a temp file to back-up into
	tempFile, err := os.CreateTemp("", m.backupFilePattern)
	if err != nil {
		return "", errors.Wrapf(err, "unable to create temp file")
	}

	backupPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return "", errors.Wrapf(err, "unable to close temp file")
	}

	// connect to source to populate sqlite connection
	sourceDb, err := m.connectToDb(ctx, m.dbPath)
	if err != nil {
		return "", errors.Wrap(err, "open source database")
	}

	defer func(sourceDb *sql.DB) {
		err := sourceDb.Close()
		if err != nil {
			log.Printf("unable to close source connection: %s", err)
		}
	}(sourceDb)

	// connect to backup to populate sqlite connection
	backupDb, err := m.connectToDb(ctx, backupPath)
	if err != nil {
		return "", errors.Wrap(err, "open backup database")
	}

	defer func(sourceDb *sql.DB) {
		err := backupDb.Close()
		if err != nil {
			log.Printf("unable to close source connection: %s", err)
		}
	}(sourceDb)

	// validate connection length
	if len(m.conns) != 2 {
		return "", errors.Wrapf(err, "no connection found to backup")
	}
	var srcDbConn = m.conns[0]
	var backupDbConn = m.conns[1]

	err = runBackup(backupDbConn, srcDbConn, m.dbName)
	if err != nil {
		return "", errors.Wrapf(err, "unable to start backup db at %s", backupPath)
	}
	return backupPath, nil
}

func runBackup(backupDbConn *sqllite.SQLiteConn, srcDbConn *sqllite.SQLiteConn, dbName string) error {
	backup, err := backupDbConn.Backup(dbName, srcDbConn, dbName)
	if err != nil {
		return errors.Wrap(err, "unable to start backup db")
	}
	defer func(backup *sqllite.SQLiteBackup) {
		err := backup.Close()
		if err != nil {
			log.Printf("unable to close backup connection: %s", err)
		}
	}(backup)

	var isDone = false
	var stepError error = nil
	for !isDone {
		isDone, stepError = backup.Step(1)
		if stepError != nil {
			return errors.Wrap(stepError, "unable to backup db at %s")
		}
	}
	return nil
}
