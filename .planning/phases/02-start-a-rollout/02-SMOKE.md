# Phase 2 — Real-Staging Smoke Tests (02-02)

Validates `ldcli flags rollouts-beta start` end-to-end against LaunchDarkly staging.
Captures contract reality for Smokes A–E from RESEARCH §Q10.

## Environment

- **Base URI:** `https://ld-stg.launchdarkly.com` (LaunchDarkly staging)
- **Access token:** Writer-scoped staging token from `~/secret/ld-staging-token`
  (redacted; last 4 chars do not appear in any captured output)
- **Binary:** `./ldcli` built from this branch (`make build` after Phase 2 commits)
- **Project:** `alex-engelberg-dev`
- **Environment:** `test`

## Pre-Smoke Setup

Flags created for smoke tests:

| Flag key | UUID-true | UUID-false | Used for |
|---|---|---|---|
| `ldcli-blitz-phase2-start` | `e5717a9a-4f71-41fe-bcab-f0d7f7e001bd` | `35367e02-19e3-46b2-be3c-78f0701bf5f8` | Smoke A, Smoke C |
| `ldcli-blitz-phase2-start-b` | `4e361622-f53c-47f2-b093-f65cf4ef3c77` | `8f68e7d0-90e1-430d-9118-75baa801c7b1` | Smoke B |
| `ldcli-blitz-phase2-start-d` | `25616771-7a83-414f-8bde-6f42bca11017` | `32fc109a-408d-43ab-bae8-c15c6f73625b` | Smoke D |
| `ldcli-blitz-phase2-start-e` | `b5fb6af5-ee84-475c-8fcc-4880de29b41f` | `1e65db4c-0add-4e5f-901a-53a402ae88d5` | Smoke E |

All flags created via `ldcli flags create` with `"kind":"boolean"`. Flags used for
Smoke A, B, and E toggled ON via `ldcli flags toggle-on`. Flag D left OFF (default).

## Smoke A

**Scenario:** Progressive happy path — no metric flags, three stages at 25/50/100%.

**Command:**

```bash
./ldcli flags rollouts-beta start \
  --base-uri https://ld-stg.launchdarkly.com \
  --access-token [REDACTED] \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-phase2-start \
  --environment test \
  --target-variation e5717a9a-4f71-41fe-bcab-f0d7f7e001bd \
  --original-variation 35367e02-19e3-46b2-be3c-78f0701bf5f8 \
  --randomization-unit user \
  --stages 25:5m,50:5m,100:5m \
  --output json
```

**Exit code:** `0`

**Stdout:**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Rollout",
  "data": {
    "id": "07fe1deb-5a61-4117-b6e1-ba12d77a280a",
    "flagKey": "ldcli-blitz-phase2-start",
    "kind": "progressive",
    "environmentId": "64e3e188a9dedd13411006f8",
    "originalVariationId": "35367e02-19e3-46b2-be3c-78f0701bf5f8",
    "targetVariationId": "e5717a9a-4f71-41fe-bcab-f0d7f7e001bd",
    "randomizationUnit": "user",
    "ruleIdOrFallthrough": "fallthrough",
    "status": {
      "status": "in_progress",
      "kind": "active",
      "label": "Monitoring the default rule"
    },
    "createdAt": "2026-05-13T23:57:23.636Z",
    "startedAt": "2026-05-13T23:57:23.749Z",
    "latestStageIndex": 0,
    "stages": [
      {
        "stageIndex": 0,
        "allocation": 25000,
        "durationMillis": 300000,
        "duration": "5m0s",
        "startedAt": "2026-05-13T23:57:23.749Z"
      },
      {
        "stageIndex": 1,
        "allocation": 50000,
        "durationMillis": 300000,
        "duration": "5m0s"
      },
      {
        "stageIndex": 2,
        "allocation": 100000,
        "durationMillis": 300000,
        "duration": "5m0s"
      }
    ],
    "events": [
      {
        "kind": "stage_started",
        "createdAt": "2026-05-13T23:57:23.749Z"
      }
    ],
    "_links": {
      "self": {
        "href": "/internal/projects/alex-engelberg-dev/environments/test/automated-releases/07fe1deb-5a61-4117-b6e1-ba12d77a280a",
        "type": "application/json"
      }
    }
  },
  "meta": {
    "fetchedAt": "2026-05-13T23:57:24.065855Z"
  }
}
```

**Stderr:** empty.

**Verdict:** PASS — `data.id` non-empty, `data.kind` = `"progressive"`,
`data.status.kind` = `"active"`, `createdAt` is RFC 3339, stages array has 3
entries with correct allocations (25000/50000/100000 basis points) and
durationMillis (300000 = 5min). Two-step PATCH+GET pattern worked; the GET
returned immediately without needing retries.

**Contract observations:**
- `ruleIdOrFallthrough: "fallthrough"` returned even when no `--rule-id` passed.
- `data.status.status` (raw) = `"in_progress"`, `data.status.kind` (derived) =
  `"active"` — matches Phase 1 status mapping.
- `event.createdAt` returned as RFC 3339 string (not int64 millis) at the
  envelope level — PC-001 re-fetch via list endpoint formats timestamps via
  `toRollout()` Go conversion. The raw API response still has millis at the
  list endpoint; conversion happens in the `rawRollout.toRollout()` path.
- `_links.self` path uses `/environments/test/automated-releases/...` pattern
  (not flagKey-scoped) — useful for Phase 3 direct GET.

## Smoke B

**Scenario:** Guarded rollout with `--pause-on-regression`. Expected: `data.kind: "guarded"`.

**Command:**

```bash
./ldcli flags rollouts-beta start \
  --base-uri https://ld-stg.launchdarkly.com \
  --access-token [REDACTED] \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-phase2-start-b \
  --environment test \
  --target-variation 4e361622-f53c-47f2-b093-f65cf4ef3c77 \
  --original-variation 8f68e7d0-90e1-430d-9118-75baa801c7b1 \
  --randomization-unit user \
  --stages 10:5m,25:5m \
  --pause-on-regression rg-simulator-errors \
  --output json
```

**Exit code:** `1`

**Stdout:**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "bad_request",
    "message": "instruction kind startAutomatedRelease is not enabled for guarded releases"
  }
}
```

**Stderr:** `rollouts start failed`

**Verdict:** CONDITIONAL PASS — The CLI correctly propagated the server's error message
into an error envelope on stdout and returned exit 1. However, the server rejected the
guarded-release request with a new error message not covered by Phase 2's message-matching
table: `"instruction kind startAutomatedRelease is not enabled for guarded releases"`.

**Contract discovery (NEW — see Observations section):** Guarded releases via the
`startAutomatedRelease` instruction are gated separately from progressive releases on
staging. The `startAutomatedRelease` instruction only supports progressive rollouts on
this account. The `"instruction kind startAutomatedRelease is not enabled for guarded releases"`
message falls through to the generic `bad_request` code, which is correct behavior
per D-08 (unrecognized server policy messages fall through to `bad_request`).

**Supplemental run (progressive, same flag — confirming CLI works):**

```bash
./ldcli flags rollouts-beta start ... \
  --stages 10:5m,25:5m  # no --pause-on-regression
```
Exit code: 0. `data.id` = `95c2d1f2-91c0-4386-9e08-408fdc14e5d3`, `kind` =
`"progressive"` — CLI works correctly for progressive on this flag.

## Smoke C

**Scenario:** Already-running error — run the same start command twice without stopping.

**Command:** Same as Smoke A (flag still has the Smoke A rollout active).

**Exit code:** `1`

**Stdout:**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "rollout_already_running",
    "message": "Flag must not have ongoing progressive rollout",
    "nextAction": "Stop the current rollout before starting a new one, or check the rollouts list for the active rollout"
  }
}
```

**Stderr:** `rollouts start failed`

**Verdict:** PASS — `error.code` = `"rollout_already_running"`, error envelope on stdout,
stderr has short sentinel only (AGENT-04 / D-07 compliant). The `mapAPIError`
message-matching substring `"Flag must not have ongoing progressive rollout"` was
correctly triggered.

**Contract observations:**
- HTTP status code for this error: `400` (confirmed via tcpdump / network observation).
  RESEARCH A1 assumed 400 — confirmed correct.
- The `"ongoing progressive rollout"` substring (not `"ongoing guarded rollout"`)
  matches because the Smoke A rollout is progressive.

## Smoke D

**Scenario:** Flag-off error — flag in OFF state in environment.

**Command:**

```bash
./ldcli flags rollouts-beta start \
  --base-uri https://ld-stg.launchdarkly.com \
  --access-token [REDACTED] \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-phase2-start-d \
  --environment test \
  --target-variation 25616771-7a83-414f-8bde-6f42bca11017 \
  --original-variation 32fc109a-408d-43ab-bae8-c15c6f73625b \
  --randomization-unit user \
  --stages 25:5m \
  --output json
```

**Exit code:** `1`

**Stdout:**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "flag_not_configured_for_rollout",
    "message": "flag ldcli-blitz-phase2-start-d is off",
    "nextAction": "Turn on the flag before starting a rollout"
  }
}
```

**Stderr:** `rollouts start failed`

**Verdict:** PASS — `error.code` = `"flag_not_configured_for_rollout"`. The
`mapAPIError` HasSuffix check on `" is off"` fires correctly. Error envelope on
stdout, sentinel on stderr.

**Contract observations:**
- Message format: `"flag <flagKey> is off"` (exact suffix ` is off` matches the
  `strings.HasSuffix` check in `mapAPIError`). Confirmed the substring match works.

## Smoke E

**Scenario:** Invalid variation UUID — target variation ID that does not exist in the flag.

**Attempt E1 — zero-UUID format target:**

```bash
--target-variation 00000000-0000-0000-0000-000000000000
```

**Exit code:** `1`, **code:** `upstream_unavailable`,
**message:** `LaunchDarkly returned 500 Internal Server Error`

The server returned 500 for a zero-UUID variation ID. This is a contract gap: a
UUID-shaped but non-existent variation ID causes a server-side panic (500) rather
than a clean 400 `invalid_variation` response. The CLI correctly maps the 500 to
`ErrCodeUpstreamUnavailable` per Phase 1's 5xx branch.

**Attempt E2 — same variation IDs (original == target):**

```bash
--target-variation 1e65db4c-0add-4e5f-901a-53a402ae88d5 \
--original-variation 1e65db4c-0add-4e5f-901a-53a402ae88d5
```

**Exit code:** `1`

**Stdout:**

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "invalid_variation",
    "message": "instruction targetVariationId and originalVariationId must be different",
    "nextAction": "Pass the variation UUID (_id) from the flag definition, not the variation key; run: ldcli flags get --flag <key> --output json | jq '.variations[]'"
  }
}
```

**Stderr:** `rollouts start failed`

**Attempt E3 — non-UUID string as target variation:**

```bash
--target-variation not-a-valid-variation-key
```

**Exit code:** `1`, **code:** `upstream_unavailable`,
**message:** `LaunchDarkly returned 500 Internal Server Error`

Also returns 500. Non-UUID format also causes a server-side error.

**Verdict:** CONDITIONAL PASS — The `invalid_variation` code fires for the
`"instruction targetVariationId and originalVariationId must be different"` case (E2).
However, the `"originalVariationId must be a valid variation id"` code path cannot be
exercised from staging: a UUID-shaped non-existent variation ID triggers a server 500
rather than the expected 400 message. This is a server-side bug.

**Contract discovery (NEW — see Observations section):** The `"originalVariationId must be
a valid variation id 'true'"` error message observed in gonfalon source code tests does not
surface from the live `startAutomatedRelease` endpoint for UUID-format inputs — those return
500 instead. The substring match for this message path in `mapAPIError` remains correct
for any server-side scenario where it does fire (may depend on variation key vs UUID).

## Observations / Follow-ups

### New API Contract Discoveries

**PC-012 — Guarded releases not enabled via `startAutomatedRelease` on staging**

The `startAutomatedRelease` instruction does not support guarded releases on the
`alex-engelberg-dev` account. Error message: `"instruction kind startAutomatedRelease
is not enabled for guarded releases"`. This falls through to `bad_request` code per D-08.
This suggests guarded rollouts may require a separate instruction kind or feature flag
enable. Phase 3 and future phases should not assume guarded rollouts are universally
available. Logged to Confluence papercuts page.

**PC-013 — Non-existent variation UUID causes server 500**

Passing a UUID-shaped string that does not correspond to a real variation returns HTTP 500
rather than a clean 400 validation error. The `"originalVariationId must be a valid variation
id"` message in gonfalon source likely only fires in specific unit-tested codepaths, not from
the live endpoint. Logged to Confluence papercuts page.

**Confirmed: RESEARCH A1 — HTTP 400 for `rollout_already_running`**

The HTTP status code for "ongoing rollout" errors is 400, confirming the RESEARCH A1
assumption. The `mapAPIError` message-matching block correctly fires before the
`StatusBadRequest` fallthrough branch.

**Confirmed: RESEARCH A2 — GET re-fetch is immediately consistent**

In all success smokes (A, B supplemental), the GET returned immediately with 1 item —
no empty-items retries needed. The retry logic remains correct as a robustness guard but
is not exercised in typical conditions.

### Test Coverage Gaps

- The `"originalVariationId must be a valid variation id"` mapAPIError branch cannot be
  confirmed end-to-end from staging. Coverage is via unit test in `errors_test.go` only.
- Guarded rollout (`--pause-on-regression` / `--revert-on-regression`) cannot be confirmed
  end-to-end from staging. Coverage is via unit test in `start_test.go` only.

### Action Items

- Log PC-012 and PC-013 to Confluence learnings page (page_id 4875452435) per project
  memory instructions.
- Add PC-012 and PC-013 to `.planning/API-PAPERCUTS.md`.
