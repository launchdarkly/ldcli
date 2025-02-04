package db

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

type backupManager struct {
	dbPath     string
	driverName string
	mutex      sync.Mutex
	conns      []*sqllite.SQLiteConn
}

func newBackupManager(dbPath string) *backupManager {
	count := c.Add(1)
	m := &backupManager{
		dbPath:     dbPath,
		driverName: fmt.Sprintf("sqlite3-backups-%d", count),
		conns:      make([]*sqllite.SQLiteConn, 0),
		mutex:      sync.Mutex{},
	}
	sql.Register(m.driverName, &sqllite.SQLiteDriver{
		ConnectHook: func(conn *sqllite.SQLiteConn) error {
			m.conns = append(m.conns, conn)
			return nil
		},
	})
	return m
}

func (m *backupManager) resetConnections() {
	m.conns = make([]*sqllite.SQLiteConn, 0)
}

func (m *backupManager) makeBackupFile(ctx context.Context) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.resetConnections()

	tempFile, err := os.CreateTemp("", "ld_cli_*.bak")
	if err != nil {
		return "", errors.Wrapf(err, "unable to create temp file")
	}

	backupPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		return "", errors.Wrapf(err, "unable to close temp file")
	}

	sourceDb, err := sql.Open(m.driverName, m.dbPath)
	if err != nil {
		return "", errors.Wrap(err, "open source database")
	}
	backupDb, err := sql.Open(m.driverName, backupPath)
	if err != nil {
		return "", errors.Wrap(err, "open backup database")
	}

	err = sourceDb.PingContext(ctx)
	if err != nil {
		return "", errors.Wrap(err, "database unreachable")
	}
	err = backupDb.PingContext(ctx)
	if err != nil {
		return "", errors.Wrap(err, "database unreachable")
	}
	if len(m.conns) != 2 {
		return "", errors.Wrapf(err, "no connection found to backup")
	}
	var srcDbConn = m.conns[0]
	var backupDbConn = m.conns[1]

	backup, err := backupDbConn.Backup("main", srcDbConn, "main")
	if err != nil {
		return "", errors.Wrapf(err, "unable to start backup db at %s", backupPath)
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
			return "", errors.Wrapf(stepError, "unable to backup db at %s", backupPath)
		}
	}

	return backupPath, nil
}

func runBackup(ctx context.Context, backupDbConn *sqllite.SQLiteConn, srcDbConn *sqllite.SQLiteConn) error {
	backup, err := backupDbConn.Backup("main", srcDbConn, "main")
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
