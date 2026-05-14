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

// DismissRegression dismisses an active regression on a guarded rollout so it can
// resume. The upstream dismissRegression instruction returns 204 No Content with no
// useful state (PAPERCUT: PC-007), so the CLI does:
//  1. The PATCH (success = 204).
//  2. An initial re-fetch via List(Limit:1, env-filtered) to surface the current state.
//  3. If the initial re-fetch already shows the dismissal landed (Status.Kind != "regressed"),
//     return immediately.
//  4. Otherwise, a bounded-backoff polling loop (1s + 3s + 5s = ~9s budget) via Get,
//     returning as soon as Status.Kind transitions out of "regressed".
//  5. If the budget is exhausted with the rollout still in "regressed", return the
//     stale rollout AND a non-empty []string warnings so the command layer can
//     surface the eventual-consistency context in the envelope's meta.warnings.
//
// The polling budget (1s/3s/5s, ~10s timeout) is derived from RESEARCH.md
// architecture Anti-Pattern 3. Plan 04-03 smoke will measure whether this is right
// empirically and record findings in CLI-LEARNINGS.md. The budget is intentionally
// NOT configurable via a CLI flag (out of scope per prototype framing).
func (c RolloutsClient) DismissRegression(
	ctx context.Context,
	accessToken, baseURI, projKey, flagKey, envKey string,
	instr DismissRegressionInstruction,
) (*Rollout, []string, error) {
	// --- Step 1: PATCH with semantic-patch envelope ---
	patch := SemanticPatch{
		EnvironmentKey: envKey,
		Instructions:   []interface{}{instr},
	}

	bodyBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, nil, errors.NewErrorWrapped("failed to marshal semantic-patch body", err)
	}

	patchPath := fmt.Sprintf("%s/api/v2/flags/%s/%s",
		strings.TrimRight(baseURI, "/"),
		url.PathEscape(projKey),
		url.PathEscape(flagKey),
	)

	patchReq, err := retryablehttp.NewRequestWithContext(ctx, "PATCH", patchPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, nil, errors.NewErrorWrapped("failed to build PATCH request", err)
	}
	// Use setStartHeaders (NOT setStandardHeaders) — the semantic-patch Content-Type
	// domain-model parameter is required by the flag PATCH endpoint (Pitfall 1, RESEARCH.md).
	c.setStartHeaders(patchReq, accessToken)

	patchResp, err := c.httpClient.Do(patchReq)
	if err != nil {
		return nil, nil, mapTransportError(err)
	}
	defer patchResp.Body.Close()

	patchBody, err := io.ReadAll(patchResp.Body)
	if err != nil {
		return nil, nil, errors.NewErrorWrapped("failed to read PATCH response body", err)
	}

	if patchResp.StatusCode >= 400 {
		return nil, nil, mapAPIError(patchBody, patchResp.StatusCode)
	}
	// Do NOT attempt to decode the PATCH response body — per PC-007 the upstream returns
	// 204 No Content with no useful state; reading the body would waste bandwidth and
	// produce a json.Unmarshal error masking the success path.

	// --- Step 2: Initial re-fetch via List(Limit:1, env-filtered) ---
	// PAPERCUT: PC-007 — upstream returns 204 No Content with no state; the CLI does an
	// explicit re-fetch loop to surface the post-dismiss state.
	list, err := c.List(ctx, accessToken, baseURI, projKey, flagKey, ListOpts{
		Environment: envKey,
		Limit:       1,
	})
	if err != nil {
		return nil, nil, err
	}
	if list == nil || len(list.Items) == 0 {
		// Defensive guard: the rollout we just dismissed should exist.
		return nil, nil, errors.NewError(
			fmt.Sprintf("rollout disappeared between dismiss PATCH and post-dismiss re-fetch for flag %q in environment %q", flagKey, envKey),
		)
	}

	rollout := &list.Items[0]

	// If the very first re-fetch already shows the dismissal landed, return immediately.
	if rollout.Status.Kind != "regressed" {
		return rollout, nil, nil
	}

	// --- Step 3: Bounded-backoff polling loop ---
	// PC-007 polling loop: backoff 1s, 3s, 5s (cumulative ~9s, capped at 10s) then give up.
	// RESEARCH.md architecture Anti-Pattern 3 suggested 1s/3s backoff + ~10s timeout;
	// Plan 04-03 smoke will MEASURE whether this is right and record in CLI-LEARNINGS.
	backoffs := []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second}
	for _, wait := range backoffs {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(wait):
		}
		// Re-fetch via Get (we have the rollout ID + env) — Get is cheaper than List.
		r, err := c.Get(ctx, accessToken, baseURI, projKey, envKey, rollout.ID)
		if err != nil {
			return nil, nil, err
		}
		rollout = r
		if rollout.Status.Kind != "regressed" {
			return rollout, nil, nil // dismissal reflected
		}
	}

	// Polling budget exhausted; return the stale rollout WITH a meta.warnings string the
	// command layer can surface so the operator knows the dismissal didn't propagate
	// within the bounded window. Status code is still success (the PATCH succeeded;
	// the eventual-consistency window is upstream's behavior — PC-007 in spirit).
	return rollout, []string{
		"Dismissal patch succeeded but the rollout's regressed state did not clear within the polling budget (~9s); see API-PAPERCUTS.md PC-007 for the upstream eventual-consistency context. Re-invoke `status` to confirm propagation.",
	}, nil
}
