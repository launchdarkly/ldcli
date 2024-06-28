package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func GetServerFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store := model.StoreFromContext(ctx)
	projectKey := GetProjectKeyFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		panic(errors.Wrap(err, "unable to get dev project"))
	}
	allFlags, err := project.GetFlagStateWithOverridesForProject(ctx)
	if err != nil {
		panic(errors.Wrap(err, "failed to get flag state"))
	}
	var body interface{}
	if flagKey, ok := mux.Vars(r)["flagKey"]; ok {
		body = ServerFlagsFromFlagsState(allFlags)[flagKey]
	} else {
		body = ServerAllPayloadFromFlagsState(allFlags)
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		panic(errors.Wrap(err, "failed to marshal flag state"))
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonBody)
	if err != nil {
		panic(errors.Wrap(err, "unable to write response"))
	}
}
