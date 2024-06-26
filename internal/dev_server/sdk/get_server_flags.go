package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func StreamServerAllPayload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store := model.StoreFromContext(ctx)
	projectKey := GetProjectKeyFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		panic(errors.Wrap(err, "unable to get dev project"))
	}
	allFlags := project.GetFlagStateWithOverridesForProject(ctx, nil) // TODO fetch overrides
	serverFlags := ServerAllPayloadFromFlagsState(allFlags)
	jsonBody, err := json.Marshal(serverFlags)
	if err != nil {
		panic(errors.Wrap(err, "failed to marshal flag state"))
	}
	updateChan, doneChan := OpenStream(w, r.Context().Done(), Message{"put", jsonBody}) // TODO Wireup updateChan
	defer close(updateChan)
	err = <-doneChan
	if err != nil {
		panic(errors.Wrap(err, "stream failure"))
	}

}
