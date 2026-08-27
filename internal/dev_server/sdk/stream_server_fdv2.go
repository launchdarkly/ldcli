package sdk

import (
	"net/http"
)

// StreamV2 serves the FDv2 streaming endpoint for server-side SDKs, which receive full
// flag configurations and evaluate them themselves.
func StreamV2(w http.ResponseWriter, r *http.Request) {
	serveFdv2Stream(w, r, fdv2ServerObjects)
}
