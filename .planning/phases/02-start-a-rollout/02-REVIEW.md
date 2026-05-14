---
phase: 02-start-a-rollout
reviewed: 2026-05-14T00:12:36Z
depth: standard
files_reviewed: 14
files_reviewed_list:
  - cmd/cliflags/flags.go
  - cmd/flags/rollouts/plaintext.go
  - cmd/flags/rollouts/rollouts.go
  - cmd/flags/rollouts/start.go
  - cmd/flags/rollouts/start_test.go
  - internal/rollouts/client.go
  - internal/rollouts/envelope.go
  - internal/rollouts/errors.go
  - internal/rollouts/errors_test.go
  - internal/rollouts/instructions.go
  - internal/rollouts/mock_client.go
  - internal/rollouts/start.go
  - internal/rollouts/start_test.go
  - internal/rollouts/testdata/start_success.json
findings:
  critical: 2
  warning: 3
  info: 2
  total: 7
status: issues_found
---

# Phase 02: Code Review Report

**Reviewed:** 2026-05-14T00:12:36Z
**Depth:** standard
**Files Reviewed:** 14
**Status:** issues_found

## Summary

Reviewed the Phase 2 "start a rollout" implementation: the `flags rollouts-beta start` command, its supporting internal client methods, error mapping, and plaintext renderer. The implementation is generally well-structured and follows existing project conventions.

Two blockers were found:

1. An index-out-of-range panic in `formatStage` — the `current` display variable is clamped but the raw `LatestStageIndex` used to index `r.Stages` is not.
2. An error-code misrouting bug in `mapAPIError` — the `409 Conflict` status-code case fires before the Phase 2 message-matching block, so if the server ever returns a 409 with a mutation-specific message (e.g. "Flag must not have ongoing guarded rollout"), the caller receives `ErrCodeConflict` rather than `ErrCodeRolloutAlreadyRunning`.

Three warnings were found: negative/zero stage durations accepted without rejection, a dead staleness-check (variable captured but never evaluated), and an unreliable body-read pattern in a test that could cause flaky failures.

---

## Critical Issues

### CR-01: Index out of range panic in `formatStage` when `LatestStageIndex` is out of bounds

**File:** `cmd/flags/rollouts/plaintext.go:131-135`

**Issue:** `formatStage` clamps `current` (the display value, line 132-133) but does NOT clamp `r.LatestStageIndex` itself before using it to index into `r.Stages` on line 135. If the API returns a `latestStageIndex` that is negative or >= `len(r.Stages)` — plausible when a rollout has just completed, been reverted, or when the API has a bug — `r.Stages[r.LatestStageIndex]` panics with an index out of range. The panic propagates to the Cobra command and crashes the CLI process.

The mismatch is:
```go
current := r.LatestStageIndex + 1
if current < 1 || current > total {
    current = 1          // display value clamped ...
}
alloc := r.Stages[r.LatestStageIndex].Allocation / 1000  // ... but raw index NOT clamped
```

**Fix:** Use the clamped index for both the display value and the slice access:

```go
func formatStage(r rollouts.Rollout) string {
    total := len(r.Stages)
    if total == 0 {
        return "—"
    }
    idx := r.LatestStageIndex
    if idx < 0 || idx >= total {
        idx = 0
    }
    alloc := r.Stages[idx].Allocation / 1000
    current := idx + 1
    return fmt.Sprintf("%d of %d (%d%%)", current, total, alloc)
}
```

---

### CR-02: `409 Conflict` short-circuits Phase 2 message-matching — `ErrCodeRolloutAlreadyRunning` never fires for 409 responses

**File:** `internal/rollouts/errors.go:125-149`

**Issue:** The `switch` in `mapAPIError` checks `statusCode == http.StatusConflict` (line 125) *before* the Phase 2 message-substring cases (lines 139-162). The comment at line 136 explicitly acknowledges the API may return 400, 409, or 422 for "Flag must not have ongoing guarded rollout", but the 409 case is already consumed by the Conflict branch, so it can never reach the `ErrCodeRolloutAlreadyRunning` case. A caller receiving a 409 with that message will get `Code: "conflict"` instead of `Code: "rollout_already_running"`, breaking the contract documented in FOUND-08 / D-12.

Current evaluation order (abbreviated):
```
case statusCode == 409:         → ErrCodeConflict  (swallows the mutation message)
...
case Contains(msg, "Flag must not have ongoing..."):  → ErrCodeRolloutAlreadyRunning  (unreachable for 409)
```

**Fix:** Move the Phase 2 message-matching block to before the status-code-specific cases (or at minimum, before the 409 case), consistent with the stated intent that message content takes precedence over status code for these mutation errors:

```go
switch {
// Phase 2 mutation-specific message matching — fires before status-code branches
// because the exact status is unconfirmed (RESEARCH A1).
case strings.HasSuffix(apiBody.Message, " is off"):
    e.Code = ErrCodeFlagNotConfiguredForRollout
    ...
case strings.Contains(apiBody.Message, "Flag must not have ongoing guarded rollout"),
    strings.Contains(apiBody.Message, "Flag must not have ongoing progressive rollout"):
    e.Code = ErrCodeRolloutAlreadyRunning
    ...
case strings.Contains(apiBody.Message, "instruction kind 'startAutomatedRelease' unsupported"):
    e.Code = ErrCodeBetaGateClosed
    ...
case strings.Contains(apiBody.Message, "originalVariationId must be a valid variation id"),
    strings.Contains(apiBody.Message, "instruction targetVariationId and originalVariationId must be different"):
    e.Code = ErrCodeInvalidVariation
    ...

// Status-code-specific cases follow.
case statusCode == http.StatusUnauthorized:
    ...
case statusCode == http.StatusForbidden:
    ...
// etc.
```

---

## Warnings

### WR-01: `parseStages` accepts negative and zero-duration stages

**File:** `cmd/flags/rollouts/start.go:165-174`

**Issue:** `time.ParseDuration` happily parses negative values (`"-60m"`) and zero (`"0s"`). Neither is validated after parsing, so a user can supply `--stages 25:-60m` or `--stages 25:0s` without error. The API will receive a negative or zero `durationMillis`, which will likely produce a confusing upstream error (or silently create a broken rollout) rather than a clear CLI validation message.

**Fix:** Add a post-parse guard:

```go
if dur <= 0 {
    return nil, fmt.Errorf("duration %q must be positive (e.g. 60m, 1h30m, 300s)", durStr)
}
```

Insert this immediately after the `time.ParseDuration` call, before appending to `stages`.

---

### WR-02: `beforePatch` staleness variable is captured but never evaluated

**File:** `internal/rollouts/start.go:32,150`

**Issue:** `beforePatch := time.Now().UTC()` (line 32) is captured to enable a staleness check against the re-fetched rollout's `CreatedAt`. The comment at lines 145-150 describes the intended check and the 2-second fudge-factor. However, the actual comparison is never performed — `beforePatch` is suppressed with `_ = beforePatch` (line 150) with a comment saying "used above" (it is not). The function returns `list.Items[0]` unconditionally with no validation.

This is a correctness risk: if the environment already had a rollout for this flag/env before the PATCH was sent, the re-fetch could return that stale rollout and `Start` will return it as if it were the newly created rollout. The caller has no way to detect this.

**Fix:** Either implement the staleness check as described in the comment, or remove `beforePatch` and update the comment to accurately document the known limitation:

```go
// Note: we return items[0] (most recent by creation time after client-side sort)
// without a staleness guard. In the unlikely race where a concurrent Start fires
// before our GET, items[0] may be the concurrently-started rollout.
// See RESEARCH §Q2 for context.
r := list.Items[0]
return &r, nil
```

---

### WR-03: `TestStart_PatchBody` uses a single `Read` call that may truncate the request body

**File:** `internal/rollouts/start_test.go:148-150`

**Issue:** The test server captures the PATCH body with:

```go
b := make([]byte, r.ContentLength)
_, _ = r.Body.Read(b)
```

A single `Read` call is not guaranteed to fill the buffer — the HTTP chunked-transfer or TCP framing can deliver the data in multiple reads. If `Read` returns fewer bytes than `r.ContentLength`, `capturedBody` is zero-padded and the subsequent `json.Unmarshal(capturedBody, &patch)` will fail (or silently succeed with a partial parse), making the assertion unreliable. In practice with the loopback httptest server this rarely triggers, but it is a flaky-test risk.

**Fix:** Use `io.ReadAll` which accumulates all chunks:

```go
import "io"
...
b, err := io.ReadAll(r.Body)
require.NoError(t, err)
capturedBody = b
```

---

## Info

### IN-01: `omitempty` on `EnvelopeMeta.FetchedAt` does not suppress the zero `time.Time`

**File:** `internal/rollouts/models.go:113`

**Issue:** The struct tag `json:"fetchedAt,omitempty"` on a `time.Time` field does not suppress the zero value. `encoding/json` treats `time.Time` as a struct (not a scalar), so `omitempty` has no effect — the zero time serializes as `"0001-01-01T00:00:00Z"`. This is a latent documentation/contract issue: any future code path that constructs `EnvelopeMeta{}` without setting `FetchedAt` will emit the epoch timestamp rather than omitting the field.

The current construction paths (`NewListEnvelope`, `NewRolloutEnvelope`) always call `time.Now().UTC()`, so this does not currently produce incorrect output.

**Fix:** Either use a `*time.Time` pointer (nil pointer is omitted) or implement `MarshalJSON` on `EnvelopeMeta`. Using a pointer is simplest:

```go
FetchedAt *time.Time `json:"fetchedAt,omitempty"`
```

---

### IN-02: Declared `DryRunFlag` constant has no implementation in the `start` command

**File:** `cmd/cliflags/flags.go:38`

**Issue:** `DryRunFlag = "dry-run"` with description "Validate the change without persisting it. Returns a preview of the result." is declared in `cliflags` but is not registered or handled anywhere in `cmd/flags/rollouts/start.go`. A user who reads the constants or help output for other commands may expect `--dry-run` to be available on `start`. This is purely informational since the flag constant is shared infrastructure, but it may cause confusion.

**Fix:** If `--dry-run` is planned for a future phase, add a comment on the constant or the `initStartFlags` function noting it is deferred. If it is not planned, no action required.

---

_Reviewed: 2026-05-14T00:12:36Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
