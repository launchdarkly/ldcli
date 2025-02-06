package api

import (
	"context"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

func (s server) RestoreBackup(ctx context.Context, request RestoreBackupRequestObject) (RestoreBackupResponseObject, error) {
	err := model.RestoreDb(ctx, request.Body)
	if err != nil {
		return nil, err
	}
	return RestoreBackup200Response{}, nil
}
