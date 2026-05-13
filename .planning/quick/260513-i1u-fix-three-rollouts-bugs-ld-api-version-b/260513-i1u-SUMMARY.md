---
quick_id: 260513-i1u
type: quick
status: complete
commits:
  - sha: 162309c
    subject: "fix(rollouts): send LD-API-Version: beta header on all requests"
  - sha: 6ecf547
    subject: "fix(rollouts): surface server message for 403 forbidden responses"
  - sha: 3f23861
    subject: "fix(rollouts): emit error envelope on stdout in JSON mode (AGENT-04)"
  - sha: 03a8be9
    subject: "docs(project): require real-server validation before phase completion"
  - sha: 4f843a3
    subject: "docs(phase-01): record real-server smoke test against staging"
files_modified:
  - internal/rollouts/client.go
  - internal/rollouts/client_test.go
  - internal/rollouts/errors.go
  - internal/rollouts/errors_test.go
  - cmd/flags/rollouts/list.go
  - cmd/flags/rollouts/list_test.go
  - .planning/PROJECT.md
  - .planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md
---

# Quick Task 260513-i1u Summary

Phase 1 (`list-foundation-first-end-to-end-slice`) shipped Walking Skeleton → Real HTTP →
Full UX over three plans and passed VERIFICATION.md — but a user-driven smoke against real
staging exposed three bugs all unit tests missed. This task fixes them, encodes a
constraint to prevent future recurrence, and records real-server evidence.

## What changed

| Task | Bug / change | Commit | Outcome |
| ---- | ------------ | ------ | ------- |
| 1 | `setStandardHeaders` did not send `LD-API-Version: beta`. Internal automated-releases API requires it; staging returned 403 without it. | `162309c` | Header added; existing httptest header-assertion test extended. |
| 2 | 403 branch of `mapAPIError` hardcoded "Access denied" and discarded `apiBody.Message`. Hid the actual cause (the missing beta header). | `6ecf547` | 403 mirrors 404 pattern: server message used when present; fallback otherwise. New `errors_test.go` (white-box) covers populated / empty / missing-field cases. |
| 3 | Error envelope landed on **stderr** in `--output json` mode. Violated AGENT-04 / D-07 (agents must branch via stdout). | `3f23861` | `emitError` writes envelope to `cmd.OutOrStdout()` in JSON mode and returns a short sentinel error so Cobra still exits 1. Plaintext UX unchanged. Integration test rewritten to use `CallCmdWithStderr` and assert stdout / stderr separately; new plaintext subtest pins existing UX. |
| 4 | No constraint against future "passed unit tests, never hit staging" gaps. | `03a8be9` | New PROJECT.md Constraint: real-server validation required before phase completion; skips must be called out in SUMMARY.md. |
| 5 | No real-server evidence existed for Phase 1. | `4f843a3` | `01-SMOKE.md` captures three real staging calls; all three exit 0 with valid `RolloutList` envelopes on stdout, stderr empty. |

## Verification

- **Per-commit:** `make test` passed cleanly before each commit; `make build` succeeded.
- **End-to-end:** After all five commits, three real-server calls against
  `https://ld-stg.launchdarkly.com` returned exit 0 with the expected envelope shape
  (see `.planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md`).

## Key findings

### Surprise 1 — the smoke fixtures didn't pre-exist (Task 5)

The plan's Task 5 "Setup confirmed (do NOT redo)" section asserted that the
`alex-engelberg-dev` project and three rollout-shaped flags were pre-created on staging.
In reality the project did not exist (first call returned 404 "Project not found"), and
neither did the three flags. The project + flags were created via `ldcli projects create`
/ `ldcli flags create` during the smoke run.

More importantly, the **rollouts** for flags B and C did not pre-exist either. Creating
rollouts with realistic state (guarded-completed, guarded-regressed, progressive-active)
requires multi-step API choreography (release pipelines, metrics, stage definitions) that
is well beyond a smoke fixture. So smokes B and C exercise the same empty-list path as
smoke A — the **command surface** is validated end-to-end against staging (URL,
headers including `LD-API-Version: beta`, envelope structure, exit code, stream routing),
but the **status-mapping contract** for guarded / progressive parsing is **not** exercised
against real upstream data. This is documented in 01-SMOKE.md under "Deviations" and
"Follow-ups", with an honest note pointing at fixture-based tests in Plan 02 for parsing
coverage and suggesting a future phase create real rollouts to close the gap.

### Surprise 2 — Task 3 fix shape

The original error path returned the envelope as the error's `Error()` string and let the
root command's `fmt.Fprintln(os.Stderr, err)` write it to stderr. To route it to stdout
without losing exit code 1, the fix had to (a) print to stdout directly in `runE` and (b)
return a **short sentinel** error rather than the envelope JSON — otherwise the same
envelope would be re-emitted to stderr by `root.go`. The integration test asserts both
sides: envelope on stdout AND `err.Error()` does not contain envelope markers
(`"kind"`, `"schemaVersion"`).

### Surprise 3 — real-server envelope shape

The real staging response includes a `data._links` block (parent + self HALF-style links)
that the unit-test fixtures did not include. The CLI envelope passes it through
transparently via the `data` payload, so no parser change was needed — but it's a useful
data point for future Phase 2 work to know real responses carry HAL-style `_links`.

## Smoke results (recap)

All three calls against staging exited 0, emitted valid `RolloutList` envelopes on
stdout, and produced empty stderr. Full envelopes captured in
`.planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md`.

| Smoke | Flag                              | Exit | items | Comment |
| ----- | --------------------------------- | ---- | ----- | ------- |
| A     | `ldcli-blitz-1-no-rollout`        | 0    | 0     | Happy path validated. |
| B     | `ldcli-blitz-2-guarded-rollouts`  | 0    | 0     | Flag created during smoke; no rollouts. |
| C     | `ldcli-blitz-3-progressive-rollouts` | 0 | 0     | Flag created during smoke; no rollouts. |

## Follow-ups recommended (NOT done here)

- Create at least one real guarded and one real progressive rollout on staging via the
  underlying API (the CLI does not yet expose `start`) and re-capture smokes B and C with
  populated `data.items` so the status-mapping contract is verified against real data.
- Extend Phase verifier checklist to require an `XX-SMOKE.md` artifact before declaring a
  phase complete, per the new PROJECT.md constraint.

## Self-Check: PASSED

- All five commits exist in `git log`:
  - `162309c` fix(rollouts): send LD-API-Version: beta header on all requests
  - `6ecf547` fix(rollouts): surface server message for 403 forbidden responses
  - `3f23861` fix(rollouts): emit error envelope on stdout in JSON mode (AGENT-04)
  - `03a8be9` docs(project): require real-server validation before phase completion
  - `4f843a3` docs(phase-01): record real-server smoke test against staging
- `make test` passes (verified after each of the first three code-change commits).
- `make build` succeeds.
- `.planning/phases/01-list-foundation-first-end-to-end-slice/01-SMOKE.md` exists with
  the three real staging captures (all exit 0).
- No modifications to `.planning/STATE.md` or `.planning/ROADMAP.md` (orchestrator will
  handle those).
- No modifications to existing phase plan/summary files under
  `.planning/phases/01-list-foundation-first-end-to-end-slice/` except the new
  `01-SMOKE.md`.
