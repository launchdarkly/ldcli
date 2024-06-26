package sdk

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/pkg/errors"
)

func StreamClientFlags(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	ctx := r.Context()
	store := model.StoreFromContext(ctx)
	projectKey := GetProjectKeyFromContext(ctx)
	project, err := store.GetDevProject(ctx, projectKey)
	if err != nil {
		panic(errors.Wrap(err, "unable to get dev project"))
	}
	allFlags := project.GetFlagStateWithOverridesForProject(ctx, nil) // TODO fetch overrides
	jsonBody, err := json.Marshal(allFlags)
	if err != nil {
		panic(errors.Wrap(err, "failed to marshal flag state"))
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, err = w.Write([]byte("event: put\ndata: "))
	if err != nil {
		panic(errors.Wrap(err, "unable to write response"))
	}
	_, err = w.Write(jsonBody)
	if err != nil {
		panic(errors.Wrap(err, "unable to write response"))
	}
	flusher.Flush()
	ticker := time.NewTicker(time.Minute)
	for _ = range ticker.C {
		_, err = w.Write([]byte(":\n"))
		if err != nil {
			panic(errors.Wrap(err, "unable to write response"))
		}
		flusher.Flush()
	}
}
