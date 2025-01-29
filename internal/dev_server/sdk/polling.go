package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
)

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
