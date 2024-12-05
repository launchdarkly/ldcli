package task

import (
	"context"
	"log"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
)

func CreateOrSyncProject(ctx context.Context, projKey string, sourceEnvironmentKey string, ldCtx *ldcontext.Context) error {
	var project model.Project
	project, createError := model.CreateProject(ctx, projKey, sourceEnvironmentKey, ldCtx)
	if createError != nil {
		if errors.Is(createError, model.ErrAlreadyExists) {
			var updateErr error
			project, updateErr = model.UpdateProject(ctx, projKey, ldCtx, &sourceEnvironmentKey)
			if updateErr != nil {
				return updateErr
			}

		} else {
			return createError
		}
	}
	log.Printf("Successfully synced Initial project [%s], ", project.Key)
	return nil
}
