package sdk

import (
	"encoding/json"
	"net/http"

	"github.com/pkg/errors"
)

// PollV2 serves the FDv2 polling endpoint for server-side SDKs, which receive full flag
// configurations and evaluate them themselves.
func PollV2(w http.ResponseWriter, r *http.Request) {
	serveFdv2Poll(w, r, fdv2ServerObjects)
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
