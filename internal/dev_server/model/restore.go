package model

import (
	"context"
	"io"
)

func RestoreDb(ctx context.Context, stream io.Reader) error {
	store := StoreFromContext(ctx)
	_, err := store.RestoreBackup(ctx, stream)
	if err != nil {
		return err
	}

	projects, err := store.GetDevProjectKeys(ctx)
	if err != nil {
		return err
	}

	observers := GetObserversFromContext(ctx)

	for _, projectKey := range projects {
		project, err := store.GetDevProject(ctx, projectKey)
		if err != nil {
			return err
		}
		allFlagsWithOverrides, err := project.GetFlagStateWithOverridesForProject(ctx)
		if err != nil {
			return err
		}
		observers.Notify(SyncEvent{
			ProjectKey:    project.Key,
			AllFlagsState: allFlagsWithOverrides,
		})
	}

	return nil
}
