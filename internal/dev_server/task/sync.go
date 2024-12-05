package task

import (
	"context"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
)

func CreateOrSyncProject(ctx context.Context, projKey string, sourceEnvironmentKey string, ldCtx *ldcontext.Context) error {
	p, err := model.CreateProject(ctx, projKey, sourceEnvironmentKey, ldCtx)
	if err != nil {
		return err
	}
	println(p.Key)
	return nil
}
