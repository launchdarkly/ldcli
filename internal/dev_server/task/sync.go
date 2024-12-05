package task

import (
	"context"
	"log"

	"github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
)

type FlagValue = ldvalue.Value

type InitialProjectSettings struct {
	Enabled    bool
	ProjectKey string
	EnvKey     string
	Context    *ldcontext.Context   `json:"context,omitempty"`
	Overrides  map[string]FlagValue `json:"overrides,omitempty"`
}

func CreateOrSyncProject(ctx context.Context, settings InitialProjectSettings) error {
	if !settings.Enabled {
		return nil
	}

	log.Printf("Initial project [%s] with env [%s]", settings.ProjectKey, settings.EnvKey)
	var project model.Project
	project, createError := model.CreateProject(ctx, settings.ProjectKey, settings.EnvKey, settings.Context)
	if createError != nil {
		if errors.Is(createError, model.ErrAlreadyExists) {
			log.Printf("Project [%s] exists, refreshing data", settings.ProjectKey)
			var updateErr error
			project, updateErr = model.UpdateProject(ctx, settings.ProjectKey, settings.Context, &settings.EnvKey)
			if updateErr != nil {
				return updateErr
			}

		} else {
			return createError
		}
	}
	log.Printf("Successfully synced Initial project [%s]", project.Key)
	return nil
}
