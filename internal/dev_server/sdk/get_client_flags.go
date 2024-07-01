package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
)

func GetClientFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	allFlags, err := GetAllFlagsFromContext(ctx)
	if err != nil {
		WriteError(ctx, w, errors.Wrap(err, "failed to get flag state"))
		return
	}
	jsonBody, err := json.Marshal(allFlags)
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
