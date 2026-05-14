package rollouts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/launchdarkly/ldcli/internal/errors"
)

// Stop terminates an in-progress rollout. Two-step PATCH+GET (PC-001): the semantic-patch
// PATCH returns the updated FeatureFlag (not the rollout), so a follow-up GET via
// List(Limit:1, env-filtered) surfaces the post-stop rollout state. Mirrors Client.Start's
// shape verbatim — see start.go for the line-by-line analog.
func (c RolloutsClient) Stop(
	ctx context.Context,
	accessToken, baseURI, projKey, flagKey, envKey string,
	instr StopInstruction,
) (*Rollout, error) {
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
	// Use setStartHeaders (NOT setStandardHeaders) — the semantic-patch Content-Type
	// domain-model parameter is required by the flag PATCH endpoint (Pitfall 1, RESEARCH.md).
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
	// PAPERCUT: PC-001 — stopAutomatedRelease PATCH returns updated FeatureFlag (not the
	// Rollout), so we re-fetch via List(Limit:1, env-filtered) to surface the post-stop state.
	// Same pattern as Client.Start — PC-001 applies to all mutation instructions on the
	// flag semantic-patch endpoint.
	list, err := c.List(ctx, accessToken, baseURI, projKey, flagKey, ListOpts{
		Environment: envKey,
		Limit:       1,
	})
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Items) == 0 {
		// Defensive guard: the rollout we just stopped should exist.
		// This branch is unexpected (not part of the normal error taxonomy) — the stop
		// succeeded but the re-fetch came back empty. Surface as an internal error.
		return nil, errors.NewError(
			fmt.Sprintf("stop succeeded but re-fetch returned no rollouts for flag %q in environment %q; check rollouts list", flagKey, envKey),
		)
	}

	r := list.Items[0]
	return &r, nil
}
