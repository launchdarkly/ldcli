package sdk

import (
	"net/http"
)

// StreamClientV2 serves the FDv2 streaming endpoints for client-side SDKs:
// POST or REPORT /sdk/stream/eval and GET /sdk/stream/eval/{context}.
//
// FDv2 unifies the browser and mobile endpoints that FDv1 kept separate, so this one
// handler replaces both /eval/{envId} and /meval.
func StreamClientV2(w http.ResponseWriter, r *http.Request) {
	serveFdv2Stream(w, r, fdv2ClientObjectsForRequest(r))
}
