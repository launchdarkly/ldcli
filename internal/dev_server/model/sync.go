package model

import (
	"context"
	"log"

	"github.com/pkg/errors"

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
	SyncOnce   bool
}

func CreateOrSyncProject(ctx context.Context, settings InitialProjectSettings) error {
	if !settings.Enabled {
		return nil
	}

	log.Printf("Initial project [%s] with env [%s]", settings.ProjectKey, settings.EnvKey)
	var project Project
	project, createError := CreateProject(ctx, settings.ProjectKey, settings.EnvKey, settings.Context)
	if createError != nil {
		if !errors.As(createError, &ErrAlreadyExists{}) {
			return createError
		}

		// If set, don't resync and don't apply overrides because whatever you have locally
		// is already set up with what you want.
		if settings.SyncOnce {
			log.Printf("Project [%s] exists, but --sync-once flag is set, skipping refresh", settings.ProjectKey)
			return nil
		}

		log.Printf("Project [%s] exists, refreshing data", settings.ProjectKey)
		var updateErr error
		project, updateErr = UpdateProject(ctx, settings.ProjectKey, settings.Context, &settings.EnvKey)
		if updateErr != nil {
			return updateErr
		}
	}
	for flagKey, val := range settings.Overrides {
		_, err := UpsertOverride(ctx, settings.ProjectKey, flagKey, val)
		if err != nil {
			return err
		}
	}

	log.Printf("Successfully synced Initial project [%s]", project.Key)
	return nil
}
