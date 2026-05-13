# Phase 1 — Real-Server Smoke Test

Captured by quick task **260513-i1u** after four rollouts bugfix commits
(`162309c`, `6ecf547`, `3f23861`, `f0eec83`) landed on `ae/cli-gr`.
Validates that `ldcli flags rollouts-beta list` talks successfully to real
staging end-to-end — closing the gap that VERIFICATION.md missed (unit tests
+ httptest synthetic servers all passed, but real staging returned 403 for
the missing `LD-API-Version: beta` header and the resulting parse failure on
`event.createdAt` was masked because no httptest fixture exercised non-string
millis).

## Environment

- **Base URI:** `https://ld-stg.launchdarkly.com` (LaunchDarkly staging)
- **Access token:** Writer-scoped staging token from the LD account that owns
  the test rollouts (loaded from `~/secret/ld-staging-token`; redacted here
  but its last 4 chars do not appear in any captured output)
- **Binary:** `./ldcli` built from this branch (`make build` after the four
  bugfix commits)
- **Project:** `alex-engelberg-dev` (pre-existing in the test account)
- **Environment:** `test` (the rollouts live on `test`)

## Bugs surfaced and fixed in this gap closure

1. **Missing `LD-API-Version: beta` header** (commit `162309c`). The internal
   automated-releases endpoint requires the header — without it, every call
   returns 403 `forbidden`.
2. **403 swallowed `apiBody.Message`** (commit `6ecf547`). The hardcoded
   "Access denied; token may lack required scope" message hid the actual
   server explanation ("This API is in beta. To use it, your request must
   include the header `LD-API-Version: beta`."), wasting debugging cycles.
3. **Error envelope written to stderr in JSON mode** (commit `3f23861`).
   Violated AGENT-04 / D-07 contract that agents must branch on outcome via
   stdout.
4. **`event.createdAt` parsed as RFC 3339 but API sends int64 millis**
   (commit `f0eec83`). After the header fix unblocked real-server calls,
   smokes B and C surfaced `error.code=unknown_upstream` /
   "failed to parse response" because `Event.CreatedAt time.Time` could not
   unmarshal `1778701152042`. Fixed by mirroring the rawRollout/Rollout
   pattern: `rawEvent` with `int64 CreatedAt`, converted to `time.Time` in
   `toEvent()`. The Plan 02 fixture `list_guarded_regressed.json` carried an
   RFC 3339 string for `event.createdAt`, masking the bug — fixture updated
   to int64 millis to match real upstream.

## Smoke A — flag with no rollouts (expected: empty `data.items`)

**Command:**

```bash
./ldcli flags rollouts-beta list \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-1-no-rollout \
  --environment test \
  --output json
```

**Exit code:** `0`

**Stdout (envelope, truncated `_links`):**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutList",
  "data": {
    "items": [],
    "_links": {...}
  },
  "meta": {"fetchedAt": "2026-05-13T20:..."}
}
```

**Stderr:** empty.

**Verdict:** ✅ — empty data.items shape correct; envelope on stdout; AGENT-04
contract respected.

## Smoke B — two guarded rollouts (one regressed, one manually reverted)

**Command:**

```bash
./ldcli flags rollouts-beta list \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --environment test \
  --output json
```

**Exit code:** `0`

**Items returned:** 2.

| id (prefix) | top-level `kind` | nested `status.status` (raw) | derived `status.kind` | derived `status.label` |
|-------------|------------------|------------------------------|-----------------------|------------------------|
| `eb858e8b...` | `guarded` | `monitoring_regressed` | `regressed` | `Regressions detected on the default rule for rg-simulator-errors` |
| `5151cbfb...` | `guarded` | `manually_reverted` | `reverted` | `the default rule rolled back manually` |

**Stderr:** empty.

**Verdict:** ✅ — status mapping for `monitoring_regressed → regressed` and
`manually_reverted → reverted` produces correct (kind, label) tuples per
RESEARCH.md §Status Mapping. Label correctly surfaces the specific metric
(`rg-simulator-errors`) for the regression case via the embedded reason path.

## Smoke C — active progressive rollout (plus one previously-reverted)

**Command:**

```bash
./ldcli flags rollouts-beta list \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-3-progressive-rollouts \
  --environment test \
  --output json
```

**Exit code:** `0`

**Items returned:** 2.

| id (prefix) | top-level `kind` | nested `status.status` (raw) | derived `status.kind` | derived `status.label` |
|-------------|------------------|------------------------------|-----------------------|------------------------|
| `0f265a08...` | `progressive` | `in_progress` | `active` | `Monitoring the default rule` |
| `011e75f4...` | `progressive` | `manually_reverted` | `reverted` | `the default rule rolled back manually` |

**Stderr:** empty.

**Verdict:** ✅ — `in_progress → active` and `manually_reverted → reverted`
both mapped correctly; the active rollout label matches the "monitoring"
state phrasing in RESEARCH.md.

## Status-mapping coverage from this smoke run

| raw status | derived kind | exercised |
|------------|--------------|-----------|
| `in_progress` | active | ✅ Smoke C |
| `monitoring_regressed` | regressed | ✅ Smoke B |
| `manually_reverted` | reverted | ✅ Smoke B + C |
| (other 10 raw statuses) | (their kinds) | ❌ not exercised against real data; still covered by `status_mapping_test.go` table tests against the 13 documented raw statuses |

## Observations / follow-ups

- **`environmentKey` is empty on each item** — the API only returns
  `environmentId`, not `environmentKey`. The CLI defensively carries both
  fields on `Rollout` but the key stays empty for now. Plaintext rendering
  shows `?` for the environment column. Capture as a papercut (likely PC-NN
  in API-PAPERCUTS.md): "API does not return environmentKey on automated-
  releases items; CLI must resolve via a separate environments call or
  surface the ID-only state".
- **`--environment test` filter is currently a no-op against this endpoint**
  — the URL pattern (`/internal/projects/{p}/flags/{flag}/automated-releases`)
  is flag-scoped, not env-scoped, and the API doesn't accept an env query
  filter we can identify. Sub-bug of the missing `environmentKey` above.
  Phase 1 contract says `--environment` filters client-side; needs follow-up.
- **`event.createdAt` JSON shape was a real assumption mismatch** — fixture
  was correct-looking RFC 3339 because that's what `time.Time` round-trips
  to on the OUT side; the IN side from the API is millis. This is the kind
  of pitfall the new PROJECT.md "real-server validation" constraint exists
  to catch earlier.
- **What this run proves:** the four bugfix commits unblock the happy path
  end-to-end against real staging; status mapping for the three raw statuses
  observed produces correct (kind, label) tuples; AGENT-04 contract holds
  (envelope on stdout, exit code 0, stderr empty).
