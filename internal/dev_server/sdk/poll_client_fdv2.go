package sdk

import (
	"net/http"
)

// PollClientV2 serves the FDv2 polling endpoints for client-side SDKs:
// POST or REPORT /sdk/poll/eval and GET /sdk/poll/eval/{context}.
//
// FDv2 unifies the browser and mobile endpoints that FDv1 kept separate, so this one
// handler replaces both /sdk/evalx/{envId} and /msdk/evalx.
func PollClientV2(w http.ResponseWriter, r *http.Request) {
	serveFdv2Poll(w, r, fdv2ClientObjectsForRequest(r))
}
