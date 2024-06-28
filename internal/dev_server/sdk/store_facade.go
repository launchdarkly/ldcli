package sdk

import (
	"context"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

// WriteError writes out a given error if it's known or panics if it isn't.
// Two assumptions it's making
//   - a panic handling middleware is in use
//   - This is in the context of flag delivery which has pretty consistent semantics for what's an error across handlers.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		http.Error(w, "project not found", http.StatusNotFound)
	case err != nil:
		panic(err)
	}
}

func GetAllFlagsFromContext(ctx context.Context) (model.FlagsState, error) {
	store := model.StoreFromContext(ctx)
	projectKey := GetProjectKeyFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		return model.FlagsState{}, errors.Wrap(err, "unable to get dev project")
	}
	allFlags, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		return model.FlagsState{}, errors.Wrap(err, "unable to get flags for project")
	}
	return allFlags, nil
}
