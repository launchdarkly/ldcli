package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func PollV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store := model.StoreFromContext(ctx)
	projectKey := GetProjectKeyFromContext(ctx)

	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get project"))
		return
	}

	allFlags, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}

	response, err := buildPollResponse(projectKey, project.PayloadVersion, allFlags, r.URL.Query().Get("basis"))
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to build poll response"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to encode response"))
	}
}

func LatestAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allFlags, err := GetAllFlagsFromContext(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}
	serverFlags := ServerAllPayloadFromFlagsState(allFlags)
	enc := json.NewEncoder(w)
	err = enc.Encode(serverFlags.Data)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to encode response"))
		return
	}
}
