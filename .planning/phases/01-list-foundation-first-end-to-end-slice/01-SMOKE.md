# Phase 1 — Real-Server Smoke Test

Captured by quick task **260513-i1u** after the three rollouts-beta bugfix commits
(`162309c`, `6ecf547`, `3f23861`) landed. Validates that `ldcli flags rollouts-beta list`
talks successfully to real staging end-to-end — closing the gap that VERIFICATION.md
missed (unit tests + httptest synthetic servers all passed, but real staging returned
403 because of the missing `LD-API-Version: beta` header).

## Environment

- **Base URI:** `https://ld-stg.launchdarkly.com` (LaunchDarkly staging)
- **Access token:** Writer-scoped staging token (token redacted in this file — token
  ends `...83eb`; full value never echoed below)
- **Binary:** `./ldcli` built from this branch (`make build` after the three bugfix
  commits)
- **Project:** `alex-engelberg-dev` (created by this task — the project the plan
  assumed pre-existed did not, so it was created with the API; see "Deviations" below)
- **Environment:** `test` (auto-created with the project)

## Deviations from the plan's Setup section

The plan's Task 5 "Setup confirmed (do NOT redo)" section asserted three test flags
were pre-created in an existing `alex-engelberg-dev` project with non-trivial rollout
state. In practice on staging:

1. The `alex-engelberg-dev` project did **not** exist — the first smoke call returned
   `error.code = "not_found"`, `"message": "Project not found"`. The project was
   created via `ldcli projects create` before re-running the smokes.
2. The three flags also did not exist — they were created via `ldcli flags create`
   with the configured `--data` payload.
3. The flags carry **no rollouts**. Smoke B (expected: two guarded rollouts) and
   Smoke C (expected: one active progressive rollout) instead return the same
   `data.items: []` shape as Smoke A. The list-foundation surface is still validated
   end-to-end (URL path, headers including `LD-API-Version: beta`, envelope structure,
   exit code, stdout/stderr routing), but the **rollout-shape parsing** (status
   kind / label mapping for guarded-completed, guarded-regressed, progressive-active)
   is **not exercised against real data** by these three calls. Follow-up: a future
   phase should either start real rollouts via the API or rely on Plan 02's fixture-
   based parsing tests for that coverage.

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

**Stdout (envelope):**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutList",
  "data": {
    "items": [],
    "_links": {
      "parent": {
        "href": "/api/v2/flags/alex-engelberg-dev/ldcli-blitz-1-no-rollout",
        "type": "application/json"
      },
      "self": {
        "href": "/internal/projects/alex-engelberg-dev/flags/ldcli-blitz-1-no-rollout/automated-releases",
        "type": "application/json"
      }
    }
  },
  "meta": {
    "fetchedAt": "2026-05-13T20:08:39.549733Z"
  }
}
```

**Stderr:** empty.

## Smoke B — flag named "guarded-rollouts" (expected per plan: two guarded rollouts; actual: empty)

**Command:**

```bash
./ldcli flags rollouts-beta list \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --environment test \
  --output json
```

**Exit code:** `0`

**Stdout (envelope):**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutList",
  "data": {
    "items": [],
    "_links": {
      "parent": {
        "href": "/api/v2/flags/alex-engelberg-dev/ldcli-blitz-2-guarded-rollouts",
        "type": "application/json"
      },
      "self": {
        "href": "/internal/projects/alex-engelberg-dev/flags/ldcli-blitz-2-guarded-rollouts/automated-releases",
        "type": "application/json"
      }
    }
  },
  "meta": {
    "fetchedAt": "2026-05-13T20:08:53.118237Z"
  }
}
```

**Stderr:** empty.

## Smoke C — flag named "progressive-rollouts" (expected per plan: active progressive rollout; actual: empty)

**Command:**

```bash
./ldcli flags rollouts-beta list \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-3-progressive-rollouts \
  --environment test \
  --output json
```

**Exit code:** `0`

**Stdout (envelope):**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutList",
  "data": {
    "items": [],
    "_links": {
      "parent": {
        "href": "/api/v2/flags/alex-engelberg-dev/ldcli-blitz-3-progressive-rollouts",
        "type": "application/json"
      },
      "self": {
        "href": "/internal/projects/alex-engelberg-dev/flags/ldcli-blitz-3-progressive-rollouts/automated-releases",
        "type": "application/json"
      }
    }
  },
  "meta": {
    "fetchedAt": "2026-05-13T20:08:57.834111Z"
  }
}
```

**Stderr:** empty.

## Status-mapping notes

All three flags carry no rollouts in staging, so the production status-mapping
contract (`Rollout.kind` ∈ {guarded, progressive}, `Status.kind` ∈ {active,
regressed, completed, reverted, paused}) is **not exercised against real data** by
this smoke run. The flag names (`ldcli-blitz-2-guarded-rollouts`,
`ldcli-blitz-3-progressive-rollouts`) describe the **intent** of the test fixture,
not the realised state on the server.

What this run **does** prove end-to-end against real staging:

- The internal automated-releases endpoint (`/internal/projects/{p}/flags/{f}/automated-releases`)
  responds 200 with the expected JSON shape under the `LD-API-Version: beta` header
  (the header fix from commit `162309c` is necessary and sufficient for the happy
  path).
- The envelope wrapping is correct: `schemaVersion = "rollouts.v1beta1"`, `kind =
  "RolloutList"`, `data._links` carries `parent` + `self`, `meta.fetchedAt` is RFC
  3339 UTC.
- Exit code `0`, full envelope on stdout, stderr empty — matching the AGENT-04 / D-07
  contract for the happy path.

## Follow-ups (suggest)

- **Real rollout coverage:** create at least one guarded and one progressive rollout
  on staging (via the gonfalon API directly, since `ldcli flags rollouts-beta start`
  doesn't yet exist) and re-capture B and C with non-empty `data.items` so the
  status-mapping contract is verified against real upstream shapes.
- **Verifier extension:** add a "real-server smoke required" gate to phase
  VERIFICATION.md going forward, per the new constraint added to PROJECT.md.
- **Bugfix validation note:** all three Phase 1 bugs (LD-API-Version header, 403
  message passthrough, error envelope routing) are also covered by unit + integration
  tests; this file is the third leg of the verification stool (real server + httptest
  + unit).
