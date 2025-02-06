package api

import (
	"context"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) GetBackup(ctx context.Context, request GetBackupRequestObject) (GetBackupResponseObject, error) {
	store := model.StoreFromContext(ctx)
	backup, size, err := store.CreateBackup(ctx)
	if err != nil {
		return nil, err
	}

	return GetBackup200ApplicationvndSqlite3Response{DbBackupApplicationvndSqlite3Response{
		Body:          backup,
		ContentLength: size,
	}}, nil

}
