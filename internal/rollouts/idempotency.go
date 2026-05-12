package rollouts

import (
	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
)

// SetIdempotencyKey assigns the Idempotency-Key header on a mutation request. If `key` is
// empty, a fresh UUIDv4 is generated. The effective key (caller-supplied or generated) is
// returned so callers can echo it back in `envelope.meta` for traceability.
//
// Phase 1: wired but not exercised — no mutations exist yet. Phase 2 calls this from the
// Start instruction path; Phase 4 calls it from Stop / DismissRegression.
func SetIdempotencyKey(req *retryablehttp.Request, key string) string {
	if key == "" {
		key = uuid.NewString()
	}
	req.Header.Set("Idempotency-Key", key)
	return key
}
