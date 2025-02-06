package api

import (
	"context"
)

func (s server) RestoreBackup(ctx context.Context, request RestoreBackupRequestObject) (RestoreBackupResponseObject, error) {
	request.Body
	panic("implement me")
}
