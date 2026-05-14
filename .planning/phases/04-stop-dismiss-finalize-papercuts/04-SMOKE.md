# Phase 4 — Real-Staging Smoke Tests (stop + dismiss-regression)

Last updated: 2026-05-14

Validates that `./ldcli flags rollouts-beta stop` and `./ldcli flags rollouts-beta dismiss-regression` talk to real LaunchDarkly staging end-to-end after the 04-01 and 04-02 commits landed on `ae/cli-gr` (`2979bff`, `789ec43`, `7ccb77e`, `6bcd4a8`, `1897497`, `dfae8e2`, `8a9d017`, `060483d`). Mirrors the structure of `01-SMOKE.md`, `02-SMOKE.md`, and `03-SMOKE.md`.

## Environment

- **Base URI:** `https://ld-stg.launchdarkly.com` (LaunchDarkly staging)
- **Access token:** Writer-scoped staging token (loaded via `~/.config/ldcli/config.yml`; literal token bytes redacted from every captured Command block below — token matches `~/secret/ld-staging-token`).
- **Binary:** `./ldcli` built from this branch (`make build` on commit `89e6cc0`).
- **Project:** `alex-engelberg-dev`.
- **Environment:** `test`.
- **Flag fixtures:**
  - `ldcli-blitz-3-progressive-rollouts` — had an active 2-stage progressive rollout (id `0f265a08…`) from 2026-05-13. Used for Smokes A and C.
  - `ldcli-blitz-1-no-rollout` — empty flag, toggled on for this run; fresh progressive rollout (id `937157ef…`) started. Used for Smokes E and B.
  - `ldcli-blitz-2-guarded-rollouts` — paused-with-regression guarded rollout (id `eb858e8b…`) from 2026-05-13; `status.label` says `"the default rule paused at 50%: regressions detected for rg-simulator-errors"`. Used for Smoke D.
  - `ldcli-blitz-phase2-start-d` — toggled on for this run; fresh progressive rollouts started for Smoke G plaintext sanity.

## Smoke A — stop → target variation (roll forward)

**Command:**
```
./ldcli flags rollouts-beta stop \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-3-progressive-rollouts \
  --environment test \
  --to-variation 91bc0969-a480-483f-a8e4-1d26796a8213 \
  --output json
```
(access token + base URI sourced from `~/.config/ldcli/config.yml`; both redacted from this log.)

**Exit code:** 0
**End-to-end latency:** ~3s (start 20:48:20Z → end 20:48:23Z) — covers pre-read List + PATCH + re-fetch List.

**Stdout (full envelope, excerpt — events array truncated for brevity):**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Rollout",
  "data": {
    "id": "0f265a08-a58f-4e6f-be2f-13c347ca7092",
    "flagKey": "ldcli-blitz-3-progressive-rollouts",
    "kind": "progressive",
    "environmentId": "[REDACTED]",
    "originalVariationId": "9f356ec6-e6f3-44b3-8a89-f8128459dea2",
    "targetVariationId": "91bc0969-a480-483f-a8e4-1d26796a8213",
    "randomizationUnit": "user",
    "ruleIdOrFallthrough": "fallthrough",
    "status": {
      "status": "manually_completed",
      "kind": "completed",
      "label": "the default rule rolled forward manually"
    },
    "createdAt": "2026-05-13T19:56:37.582Z",
    "startedAt": "2026-05-13T19:56:37.708Z",
    "endedAt": "2026-05-14T20:48:22.1Z",
    "latestStageIndex": 1
  },
  "meta": {
    "fetchedAt": "2026-05-14T20:48:23.169098Z",
    "uiURL": "https://ld-stg.launchdarkly.com/alex-engelberg-dev/test/features/ldcli-blitz-3-progressive-rollouts/targeting"
  }
}
```

**Stderr:** (empty)

**Verdict:** ✓ Pass.

**Contract observations:**
- `schemaVersion == "rollouts.v1beta1"`, `kind == "Rollout"`, `data.id` matches the pre-read rollout ID.
- **`data.status.kind == "completed"`** when stopping to the TARGET variation. Status.Kind buckets the post-stop state as `completed` (not `reverted`). Empirical answer to **Q3 (stop case, target direction)**.
- `data.status.status == "manually_completed"` — separate from `kind`; this is the underlying server-side status field that the prototype's classifier bucket masks behind `kind`.
- `meta.uiURL` populated and URL-resolves to the flag's targeting tab in the LaunchDarkly UI. Empirical answer to **Q4** (see Plan 04-02 open questions answered below).
- Two-step PATCH + re-fetch took ~3s end-to-end. PC-001 workaround is fast enough that no `--wait` flag is needed.

## Smoke B — stop → original variation (roll back)

(Smoke order in this log: A → C → E → B → D → G. Smoke B ran after a fresh rollout was started on `ldcli-blitz-1-no-rollout` for Smoke E.)

**Command:**
```
./ldcli flags rollouts-beta stop \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-1-no-rollout \
  --environment test \
  --to-variation c0cf6728-a3bb-4c49-b918-f8b7fb4da57b \
  --output json
```

**Exit code:** 0
**End-to-end latency:** ~1s (start 20:50:37Z → end 20:50:38Z).

**Stdout (full envelope, excerpt):**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Rollout",
  "data": {
    "id": "937157ef-6f8c-4f09-8b51-26ee8c0464a5",
    "flagKey": "ldcli-blitz-1-no-rollout",
    "kind": "progressive",
    "environmentId": "[REDACTED]",
    "originalVariationId": "c0cf6728-a3bb-4c49-b918-f8b7fb4da57b",
    "targetVariationId": "4c6defd9-e533-4ad6-82ed-25470adb9250",
    "randomizationUnit": "user",
    "status": {
      "status": "manually_reverted",
      "kind": "reverted",
      "label": "the default rule rolled back manually"
    }
  },
  "meta": {
    "fetchedAt": "2026-05-14T20:50:38.840608Z",
    "uiURL": "https://ld-stg.launchdarkly.com/alex-engelberg-dev/test/features/ldcli-blitz-1-no-rollout/targeting"
  }
}
```

**Stderr:** (empty)

**Verdict:** ✓ Pass.

**Contract observations:**
- **`data.status.kind == "reverted"`** when stopping to the ORIGINAL variation. Combined with Smoke A this proves the prototype's bucketing aligns with the upstream behavioral semantics: target → `completed`, original → `reverted`.
- `data.status.status == "manually_reverted"` (server-side underlying status).
- Stop's `--to-variation` does NOT require the variation to be either original or target — the server accepts arbitrary variation UUIDs. The prototype does not validate this. Possible CLI/UX question for production (see CL-013).

## Smoke C — already-terminal refusal

**Command:** (re-run of Smoke A's command after Smoke A made the rollout terminal)
```
./ldcli flags rollouts-beta stop \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-3-progressive-rollouts \
  --environment test \
  --to-variation 91bc0969-a480-483f-a8e4-1d26796a8213 \
  --output json
```

**Exit code:** 1
**End-to-end latency:** <1s (only the pre-read List was issued; the PATCH was suppressed by the pre-read guard).

**Stdout (full envelope):**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "rollout_already_terminal",
    "message": "Rollout 0f265a08-a58f-4e6f-be2f-13c347ca7092 for flag \"ldcli-blitz-3-progressive-rollouts\" in environment \"test\" is already in state \"completed\"; cannot stop a terminal rollout.",
    "nextAction": "List the flag's rollouts (ldcli flags rollouts-beta list --flag <key>) to confirm the terminal state. Start a new rollout if needed."
  }
}
```

**Stderr:**
```
rollouts stop failed
```

**Verdict:** ✓ Pass.

**Contract observations:**
- Exit 1 + Error envelope on stdout + short sentinel on stderr — matches the SC#3 envelope contract.
- `error.code == "rollout_already_terminal"` is CLI-emitted (not server-emitted); the upstream `stopAutomatedRelease` instruction never fired.
- `error.message` names the current state verbatim (`"completed"`) — useful for agents that key off message inspection.

## Smoke D — dismiss against paused-with-regression rollout

**Command:**
```
./ldcli flags rollouts-beta dismiss-regression \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-2-guarded-rollouts \
  --environment test \
  --output json
```

**Exit code:** 1
**End-to-end latency:** <1s (pre-read List only; no PATCH issued).

**Stdout (full envelope):**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "no_active_regression",
    "message": "Rollout eb858e8b-cb92-474b-9666-5b82ac8dcdb5 for flag \"ldcli-blitz-2-guarded-rollouts\" in environment \"test\" is in state \"paused\"; there is no active regression to dismiss.",
    "nextAction": "Run `ldcli flags rollouts-beta status --flag <key>` to inspect the current state. Dismissal is only meaningful when the rollout is in 'regressed' state."
  }
}
```

**Stderr:**
```
rollouts dismiss-regression failed
```

**Verdict:** ⚠ Pass-but-reveals-design-gap.

**Contract observations (this is the most important smoke in the run):**
- The pre-read shows **`Status.Kind == "paused"`** — even though the rollout's `status.label` literally says `"the default rule paused at 50%: regressions detected for rg-simulator-errors"`. Regression info is encoded in `label`, not `kind`.
- A wider sweep of every rollout's history across the project's known flags (Smoke probe; see Observations / Follow-ups → PC-021) found **no rollout with `Status.Kind == "regressed"`**. Kinds seen across 12 rollouts in 5 flags: `{paused, reverted, completed, active}`. The `"regressed"` Status.Kind bucket the prototype gates on may never appear in real upstream responses.
- This means the dismiss command's pre-read (`if current.Status.Kind != "regressed"`) will reject **every** real regression scenario, and the bounded-backoff polling loop / `meta.warnings` / PC-007 timeout path will never fire empirically. Plan 04-02 open questions **#1 (polling budget rightness), #2 (instruction body shape), and full-form #3 (post-dismiss Status.Kind)** are blocked behind this gap — they cannot be answered until the dismiss pre-read is reshaped to detect regression via `label` or another field.
- See Observations / Follow-ups → PC-021 + CL-013.

## Smoke E — no-active-regression refusal (clean active state)

**Command:** (run against the fresh progressive rollout started on `ldcli-blitz-1-no-rollout` before Smoke B)
```
./ldcli flags rollouts-beta dismiss-regression \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-1-no-rollout \
  --environment test \
  --output json
```

**Exit code:** 1
**End-to-end latency:** <1s (pre-read List only; no PATCH issued).

**Stdout (full envelope):**
```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "no_active_regression",
    "message": "Rollout 937157ef-6f8c-4f09-8b51-26ee8c0464a5 for flag \"ldcli-blitz-1-no-rollout\" in environment \"test\" is in state \"active\"; there is no active regression to dismiss.",
    "nextAction": "Run `ldcli flags rollouts-beta status --flag <key>` to inspect the current state. Dismissal is only meaningful when the rollout is in 'regressed' state."
  }
}
```

**Stderr:**
```
rollouts dismiss-regression failed
```

**Verdict:** ✓ Pass.

**Contract observations:**
- Exit 1 + Error envelope on stdout + short sentinel on stderr.
- `error.code == "no_active_regression"`, state is `"active"` (the rollout was just started in its first stage).
- Latency confirms no upstream PATCH was issued (the pre-read suppressed it).

## Smoke F — bounded-backoff-timeout reproduction (skipped)

**Skipped.** Reproduction requires a rollout in `Status.Kind == "regressed"`; Smoke D + the wider-sweep probe established that this Status.Kind bucket does not appear in any real staging response observed during this milestone. Without a fixture, the bounded-backoff loop and the `meta.warnings` / PC-007 timeout path cannot be exercised against real staging. Recorded as **could not reproduce empirically**.

## Smoke G — plaintext sanity check

**First attempt** (without `FORCE_TTY=1`, stdout redirected to a file):
```
./ldcli flags rollouts-beta stop \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-phase2-start-d \
  --environment test \
  --to-variation 25616771-7a83-414f-8bde-6f42bca11017
```

This actually emitted the **JSON envelope**, not plaintext, because `OutputFlag` defaults to `json` when stdout is not a TTY (documented behavior; see `cmd/cliflags/flags.go` `OutputFlagDescription`). The same flag's behavior is captured under "default: plaintext in a terminal, json otherwise".

**Second attempt** (with `FORCE_TTY=1` to coerce plaintext):
```
FORCE_TTY=1 ./ldcli flags rollouts-beta stop \
  --project alex-engelberg-dev \
  --flag ldcli-blitz-phase2-start-d \
  --environment test \
  --to-variation 25616771-7a83-414f-8bde-6f42bca11017
```

**Exit code:** 0

**Stdout (plaintext rendering):**
```
Stopped rollout c42efcad-95d4-43a9-8353-3b1a0c62ef73 (progressive) in environment —
Status: completed
Label: the default rule rolled forward manually
```

**Stderr:**
```
⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change.
  Pin to ldcli dev for production use.
```

**Verdict:** ✓ Pass.

**Contract observations:**
- `RenderRolloutStopPlaintext` is wired up correctly; concise 3-line summary matches the function's design.
- **"environment —"** placeholder reaffirms **CL-009** (Plaintext `Env: —` when env is implicit) — this confirms the gap also exists in the stop renderer, not just the status renderer. The renderer doesn't carry the operator-supplied `--environment test` flag value through to the response shape; the upstream's `environmentId` is opaque (PC-019).
- The beta-warning banner appears on stderr only in the TTY path, not the piped-JSON path. UX is sensible.

## Plan 04-02 open questions answered

1. **Is the polling budget right? (1s/3s/5s, ~9s total)**
   **Unanswered empirically.** The pre-read refused every dismiss attempt (Smokes D + E) before the PATCH and polling loop could fire. The polling-budget logic is exercised only by the unit tests on `internal/rollouts/dismiss.go`. Until the dismiss pre-read is reshaped to detect regression via `status.label` (or a different upstream field), real-staging measurement is impossible. See PC-021 + CL-013.

2. **Does the upstream `dismissRegression` instruction body really have no fields besides `Kind`?**
   **Unanswered empirically.** No PATCH was sent. The instruction body shape is assumed-correct per `internal/rollouts/instructions.go` based on architecture research; empirical verification is blocked behind the regression-detection gap. See PC-021.

3. **What is the post-dismiss `Status.Kind` value?**
   **Inverted finding.** No "post-dismiss" state was ever reached because no real "pre-dismiss" state (`Status.Kind == "regressed"`) was found in any rollout's history (12 rollouts across 5 flags surveyed; kinds seen: `{paused, reverted, completed, active}`). **The Status.Kind taxonomy upstream does not include `"regressed"` as a separate bucket** — regression-paused rollouts surface as `Kind == "paused"` with regression info encoded in `status.label` (`"the default rule paused at 50%: regressions detected for ..."`). The dismiss command's pre-read needs to be reshaped to detect regression via `label` (or an explicit `events[].kind == "regression_detected"` scan), not via `Kind`. See PC-021 + CL-013.

   **Adjacent finding (stop case):** Stop → TARGET variation produces `Status.Kind == "completed"`; Stop → ORIGINAL variation produces `Status.Kind == "reverted"`. The prototype's bucketing aligns with upstream behavioral semantics for the stop path.

4. **Does the `BuildUIURL` path shape match the real LD UI?**
   **Answered — yes (flag-level), with caveat.** Every captured envelope's `meta.uiURL` resolves correctly in the browser. Example: `https://ld-stg.launchdarkly.com/alex-engelberg-dev/test/features/ldcli-blitz-3-progressive-rollouts/targeting`. The URL leads to the flag's targeting tab — the rollout-specific UI panel is reachable from there. The path shape is **flag-level**, not **rollout-level** — operators see the flag page rather than a deep link to a specific rollout. Acceptable for the prototype; the production CLI build may want a more precise rollout anchor (`/automated-releases/{rolloutId}` or similar) if/when the LD UI exposes one. See CL-015.

## Observations / Follow-ups

### → Task 3 (API-PAPERCUTS.md): candidate new entries

- **PC-021 — `Status.Kind` taxonomy omits `"regressed"`; regression state is hidden in `status.label`.**
  Discovered: 2026-05-14 (Phase 4 milestone; Smoke D + history sweep across 12 rollouts in 5 flags).
  API behavior: When a guarded rollout's monitor detects a regression, the upstream returns `status.kind == "paused"` with `status.label` containing `"regressions detected for ..."`. There is no `Status.Kind == "regressed"` enum value visible in any observed response. The `events[]` array contains a `regression_detected` event with the offending `metricKey`, but the top-level Kind classifier flattens this distinction.
  CLI workaround: The dismiss-regression command's pre-read currently gates on `Status.Kind == "regressed"` — this rejects every real regression scenario observed on staging. Workaround for the prototype: keep the gate as-is and document the gap. Production CLI build needs to reshape the gate to detect regression via `label` substring match OR by scanning `events[]` for a `regression_detected` event newer than the latest `regression_dismissed`/`safe_roll_forward` event.
  What we'd prefer: a top-level `Status.Kind == "regressed"` bucket (or an explicit `data.activeRegression: bool` field) so downstream consumers don't have to substring-parse `label` or scan events.
  Status: active.
  Removal criteria: upstream exposes a stable "is this rollout currently in an unresolved regression?" predicate; the dismiss pre-read can read from it directly.

### → CLI-LEARNINGS.md (LEARN-02): candidate new + extended entries

- **CL-013 (new) — Dismiss pre-read gates on the wrong field.**
  Question: how should the CLI detect "is this rollout currently in an unresolved regression?" — Status.Kind, status.label, events[], or a future explicit predicate?
  What we did in prototype: gated on `Status.Kind == "regressed"`. Real staging never produces that Kind value (Smoke D); the gate rejects every real regression scenario.
  What's open for production CLI build: reshape the gate to detect regression via `status.label` substring match OR by scanning `events[]` for a `regression_detected` event without a subsequent `regression_dismissed`/`safe_roll_forward`. Coordinate with the API team on PC-021 — the cleanest fix is a stable upstream predicate. Until then, prefer `events[]` scanning over `label` substring matching.
  Severity: high (prevents the command from doing its core job on real staging until the API team or the next CLI iteration fixes it).

- **CL-014 (new) — `stop --to-variation` accepts any variation UUID, not just original/target.**
  Question: should the CLI validate that `--to-variation` matches either the rollout's `originalVariationId` or `targetVariationId`, or should it accept arbitrary UUIDs and let the server validate?
  What we did in prototype: pass-through; no validation. Smoke B confirmed the upstream accepts any UUID (the variation we chose happened to be the original — but a typo'd or wrong-flag UUID would also pass the CLI and likely produce a server-side error or, worse, a silent stop to an unintended variation).
  What's open for production CLI build: consider validating `--to-variation` against the flag's variations list via a pre-flight `flags get` lookup. Alternative: ship higher-level `--rollback` / `--roll-forward` flags that resolve to the original/target UUID automatically (less footgun risk; matches operator intent more directly).
  Severity: medium (operational footgun; not a security issue, but easy to mis-target).

- **CL-015 (new) — `meta.uiURL` path shape is flag-level, not rollout-level.**
  Question: should the UI permalink point at the flag's targeting tab (current behavior) or at a rollout-specific anchor (e.g., `/automated-releases/{rolloutId}`)?
  What we did in prototype: `BuildUIURL` constructs `…/features/{flagKey}/targeting`. Verified end-to-end against staging (Smoke A, B, G); the URL resolves to the flag's targeting page, from which operators can see the active rollout. Not a wrong answer, but not maximally precise.
  What's open for production CLI build: investigate whether the LD UI exposes a stable per-rollout anchor (e.g., `/features/{flagKey}/targeting?rollout={rolloutId}`). Coordinate with the UI team.
  Severity: low (the current URL works; this is a quality-of-life improvement).

### → CLI-LEARNINGS.md: appends to existing entries

- **CL-008 (Typed Go structs strip wire fields) — Phase 4 confirmation:** Smoke G's plaintext rendering omits `data.status.status` (the underlying `manually_completed` / `manually_reverted` field), `data.endedAt`, and `data.events[]` from the user-visible output even though they're present in the JSON envelope. Same pattern observed in Phase 3 for status; reaffirmed for stop. No new fix needed; just adds a data point.

- **CL-009 (Plaintext `Env: —` when env is implicit) — Phase 4 confirmation:** Smoke G's plaintext output renders `in environment —` even though `--environment test` was passed. The stop renderer doesn't carry the operator-supplied env value through to the response shape (the upstream's `environmentId` is opaque per PC-019). Same gap as the status command. No new fix needed; adds a data point.

### → Plan 04-04 (milestone close): inputs

The PC-021 entry needs to land in API-PAPERCUTS.md AND the Confluence page (page_id 4875452435) before milestone close — it is a contract-shape observation per PROJECT.md's API contract learnings rule. CL-013, CL-014, CL-015 are pure CLI/UX observations and stay local to CLI-LEARNINGS.md.

### Secondary findings (not papercut-worthy, recorded for completeness)

- `--all` on `rollouts-beta list` currently sends `limit=1000`; the upstream rejects with `bad_request: "Limit must be less than or equal to 100"`. PC-003's documented workaround is broken in some environments. Possible follow-up: have `--all` request `limit=100` instead, or use the upstream's actual max (whichever the server reports). Not raised as a new papercut since PC-003 already captures the underlying gap; flagging here so the production CLI build doesn't miss it.
