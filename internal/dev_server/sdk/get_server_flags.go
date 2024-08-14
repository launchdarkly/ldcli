package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

func GetServerFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allFlags, err := GetAllFlagsFromContext(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}
	var body interface{}
	if flagKey, ok := mux.Vars(r)["flagKey"]; ok {
		body, ok = ServerFlagsFromFlagsState(allFlags)[flagKey]
		if !ok {
			http.Error(w, "flag not found", http.StatusNotFound)
		}
	} else {
		body = ServerAllPayloadFromFlagsState(allFlags)
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to marshal flag state"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonBody)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "unable to write response"))
		return
	}
}
