package db

import (
	"context"
	"database/sql"
	sqllite "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
	"log"
)

func makeBackupFile(ctx context.Context, dbPath string) (string, error) {
	connections := []*sqllite.SQLiteConn{}
	driverName := "sqlite3-me"
	backupPath := dbPath + ".backup"
	sql.Register(driverName, &sqllite.SQLiteDriver{
		ConnectHook: func(conn *sqllite.SQLiteConn) error {
			connections = append(connections, conn)
			return nil
		},
	})

	sourceDb, err := sql.Open(driverName, dbPath)
	if err != nil {
		return "", errors.Wrap(err, "open source database")
	}
	backupDb, err := sql.Open(driverName, backupPath)
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
	if len(connections) != 2 {
		return "", errors.Wrapf(err, "no connection found to backup")
	}
	var srcDbConn = connections[0]
	var backupDbConn = connections[1]

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
