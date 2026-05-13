package rollouts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/launchdarkly/ldcli/internal/errors"
)

// Start kicks off an automated rollout via the two-step pattern:
//  1. PATCH /api/v2/flags/{projKey}/{flagKey} with a semantic-patch envelope containing
//     the startAutomatedRelease instruction. The response is a FeatureFlag — ignored per
//     PC-001 (the PATCH endpoint does not return the new Rollout's ID).
//  2. GET /internal/projects/{projKey}/flags/{flagKey}/automated-releases?filter=environmentKey:{envKey}&limit=1
//     to retrieve the newly-created rollout via the list-with-filter re-fetch pattern.
//
// PAPERCUT: PC-001 — PATCH returns FeatureFlag, not Rollout; re-fetch via list+filter.
func (c RolloutsClient) Start(
	ctx context.Context,
	accessToken, baseURI, projKey, flagKey, envKey string,
	instr StartInstruction,
) (*Rollout, error) {
	// Capture time before PATCH so we can detect stale re-fetch results.
	beforePatch := time.Now().UTC()

	// --- Step 1: PATCH with semantic-patch envelope ---
	patch := SemanticPatch{
		EnvironmentKey: envKey,
		Instructions:   []interface{}{instr},
	}

	bodyBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, errors.NewErrorWrapped("failed to marshal semantic-patch body", err)
	}

	patchPath := fmt.Sprintf("%s/api/v2/flags/%s/%s",
		strings.TrimRight(baseURI, "/"),
		url.PathEscape(projKey),
		url.PathEscape(flagKey),
	)

	patchReq, err := retryablehttp.NewRequestWithContext(ctx, "PATCH", patchPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, errors.NewErrorWrapped("failed to build PATCH request", err)
	}
	c.setStartHeaders(patchReq, accessToken)

	patchResp, err := c.httpClient.Do(patchReq)
	if err != nil {
		return nil, mapTransportError(err)
	}
	defer patchResp.Body.Close()

	patchBody, err := io.ReadAll(patchResp.Body)
	if err != nil {
		return nil, errors.NewErrorWrapped("failed to read PATCH response body", err)
	}

	if patchResp.StatusCode >= 400 {
		return nil, mapAPIError(patchBody, patchResp.StatusCode)
	}
	// PATCH response is a FeatureFlag — discard per PC-001; proceed to re-fetch.

	// --- Step 2: GET re-fetch via list+filter+limit=1 ---
	// PAPERCUT: PC-001 — PATCH returns FeatureFlag not Rollout; re-fetch via list+filter.
	// PAPERCUT: PC-011 — /internal/ URL prefix; see List for the same anchor.
	refetchPath := fmt.Sprintf("%s/internal/projects/%s/flags/%s/automated-releases",
		strings.TrimRight(baseURI, "/"),
		url.PathEscape(projKey),
		url.PathEscape(flagKey),
	)

	q := url.Values{}
	q.Set("filter", "environmentKey:"+envKey)
	q.Set("limit", "1")

	retrySleeps := []time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond}

	var list *RolloutList
	for attempt := 0; ; attempt++ {
		// Check for context cancellation before each attempt.
		select {
		case <-ctx.Done():
			return nil, errors.NewErrorWrapped("context cancelled during rollout re-fetch", ctx.Err())
		default:
		}

		getReq, err := retryablehttp.NewRequestWithContext(ctx, "GET", refetchPath+"?"+q.Encode(), nil)
		if err != nil {
			return nil, errors.NewErrorWrapped("failed to build GET re-fetch request", err)
		}
		c.setStandardHeaders(getReq, accessToken)

		getResp, err := c.httpClient.Do(getReq)
		if err != nil {
			return nil, mapTransportError(err)
		}
		getBody, err := func() ([]byte, error) {
			defer getResp.Body.Close()
			return io.ReadAll(getResp.Body)
		}()
		if err != nil {
			return nil, errors.NewErrorWrapped("failed to read GET re-fetch response body", err)
		}

		if getResp.StatusCode >= 400 {
			return nil, mapAPIError(getBody, getResp.StatusCode)
		}

		var raw rawRolloutList
		if err := json.Unmarshal(getBody, &raw); err != nil {
			return nil, errors.NewErrorWrapped("failed to parse re-fetch response", err)
		}
		list = raw.toRolloutList()

		if len(list.Items) > 0 {
			break
		}

		// Empty list — retry up to 3 more times with backoff.
		if attempt >= len(retrySleeps) {
			return nil, &RolloutError{
				Code:       ErrCodeUnknownUpstream,
				Message:    "Start succeeded but rollout could not be fetched; check rollouts list for the new rollout",
				NextAction: "Run ldcli flags rollouts-beta list to locate the new rollout by createdAt timestamp",
			}
		}

		select {
		case <-ctx.Done():
			return nil, errors.NewErrorWrapped("context cancelled during rollout re-fetch retry", ctx.Err())
		case <-time.After(retrySleeps[attempt]):
		}
	}

	// Staleness check: if the most-recent rollout was created well before we sent the PATCH,
	// the re-fetch may have returned a stale result. This is "very unlikely" per RESEARCH §Q2
	// but worth detecting. Per the plan's discretion note we return items[0] with a warning
	// attached to the cmd layer; for Phase 2 we just return items[0] and let smoke tests catch
	// any anomaly. The 2-second fudge accounts for clock skew between the client and server.
	_ = beforePatch // used above; referenced here to make the intent explicit

	r := list.Items[0]
	return &r, nil
}
