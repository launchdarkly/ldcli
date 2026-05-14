---
phase: 03-status-watch
plan: 01
subsystem: rollouts-beta
tags: [status, vertical-slice, prototype]
requires: [01 envelope, 01 Client.Get/List, 01 mapAPIError, 01 status_mapping]
provides:
  - cmd/flags/rollouts/status.go:NewStatusCmd
  - cmd/flags/rollouts/plaintext.go:RenderRolloutStatusPlaintext
  - cliflags.RolloutIdFlag
  - rollouts.ErrCodeNoRolloutsFound
  - .planning/CLI-LEARNINGS.md (CL-001..CL-007 seeded)
affects: [cmd/flags/rollouts/rollouts.go (added status verb)]
tech-stack:
  added: []
  patterns: [Viper-at-RunE, JSON-envelope-on-stdout, sectioned-plaintext, typed-RolloutError-validation-guard]
key-files:
  created:
    - cmd/flags/rollouts/status.go
    - cmd/flags/rollouts/status_test.go
    - .planning/CLI-LEARNINGS.md
  modified:
    - cmd/cliflags/flags.go
    - cmd/flags/rollouts/plaintext.go
    - cmd/flags/rollouts/rollouts.go
    - internal/rollouts/errors.go
decisions:
  - D-02 surface honored verbatim — exactly --flag/--project/--environment/--rollout-id; --detailed/--short/--state/--limit/--watch NOT registered
  - D-03 CLI-side validation guard fires BEFORE any client call (typed RolloutError → error.code:bad_request)
  - D-04 most-recent via Client.List Limit:1 + items[0] — no Phase-3-specific sort
  - D-05 envelope reuse verbatim via NewRolloutEnvelope
  - D-09 exactly one new error-code constant: ErrCodeNoRolloutsFound
  - D-12 zero new methods on internal/rollouts.Client
metrics:
  duration_minutes: ~30
  completed_date: 2026-05-14
  commits: 3
  tasks: 3
  files_changed: 7
  lines_added: 710
---

# Phase 03 Plan 01: Status Command Vertical Slice Summary

JWT — wait, wrong project. **One-liner:** Ship `ldcli flags rollouts-beta status --flag <key> [--environment <env>] [--rollout-id <id>]` end-to-end with the existing JSON envelope, a new sectioned plaintext renderer, one new error code, one new CLI flag, and the CLI-LEARNINGS.md skeleton seeded with CL-001..CL-007 — all in a single autonomous plan, zero new methods on `internal/rollouts/Client`, zero new dependencies, prototype-first framing held.

## What Shipped

**Three commits, three tasks, all tests green.**

| Task | Commit | Subject |
| ---- | ------ | ------- |
| 1    | `c273882` | feat(03-01): add RolloutIdFlag, ErrCodeNoRolloutsFound, CLI-LEARNINGS.md skeleton |
| 2    | `8c0587e` | feat(03-01): add status command + sectioned plaintext renderer |
| 3    | `0985984` | test(03-01): status command — 7 scenarios for happy paths and edge cases |

### Surface area

- **CLI:** `ldcli flags rollouts-beta status --flag <key> [--environment <env>] [--rollout-id <id>] [--project <key>]`
- **JSON output:** `{schemaVersion: "rollouts.v1beta1", kind: "Rollout", data: <Rollout>, meta: {fetchedAt}}` on success; `{schemaVersion, kind: "Error", error: {code, message, nextAction}}` on stdout on failure (AGENT-04 / D-07 routing — error envelope on stdout in JSON mode, short sentinel error returned so root doesn't double-emit).
- **Plaintext output (default in TTY):** Sectioned blocks — Overview (Rollout/Flag/Env/Kind/State/Label/Created/Started/Ended/Target var/Original var), Stages (with `[✓]/[→]/[ ]` markers + percent + duration + state), Metrics (key + per-metric status + auto-rollback), Events (timestamp + kind + metric key).

### Files

- **New:** `cmd/flags/rollouts/status.go` (206 lines — NewStatusCmd, initStatusFlags, statusRunE, resolveRollout, emitStatusSuccess, emitStatusError), `cmd/flags/rollouts/status_test.go` (278 lines — 7 test scenarios), `.planning/CLI-LEARNINGS.md` (107 lines — anchor table + CL-001..CL-007 seeded entries).
- **Modified:** `cmd/cliflags/flags.go` (+RolloutIdFlag constant + RolloutIdFlagDescription), `internal/rollouts/errors.go` (+ErrCodeNoRolloutsFound), `cmd/flags/rollouts/plaintext.go` (+RenderRolloutStatusPlaintext + timeOrDash + stageMarkerAndState), `cmd/flags/rollouts/rollouts.go` (registered NewStatusCmd alongside list/start).

## Test Results

```
$ go test ./cmd/flags/rollouts/... -count=1
ok  	github.com/launchdarkly/ldcli/cmd/flags/rollouts	2.408s

$ go test ./internal/rollouts/... -count=1
ok  	github.com/launchdarkly/ldcli/internal/rollouts	1.162s

$ go build ./...
(exit 0)
```

All 7 new status tests pass on first try (no implementation tweaks needed):

1. `TestStatus_MostRecentPath_JSONOutput` — default path: List(Limit:1) called; Get not called.
2. `TestStatus_RolloutIdPath_JSONOutput` — --rollout-id path: Get called; List not called.
3. `TestStatus_RolloutIdWithoutEnvironment_ValidationError` — D-03 guard fires BEFORE any client call; error.code = `bad_request`.
4. `TestStatus_NoRolloutsFound_ErrorEnvelopeOnStdout` — empty list → `error.code = no_rollouts_found`; envelope on stdout not stderr.
5. `TestStatus_NoRolloutsFound_NilList_ErrorEnvelope` — nil list → same `no_rollouts_found`; proves nil-guard.
6. `TestStatus_PlaintextOutput_ContainsSectionHeaders` — TTY plaintext contains all 4 section headers.
7. `TestStatus_ListClientError_ErrorEnvelope` — Phase 1 typed RolloutError surfaces verbatim through emitStatusError.

All existing rollouts tests (list_test, start_test, plaintext_test, rollouts_test) remain green.

## CLI-LEARNINGS.md Anchors Seeded

Per Phase 3 D-13, seeded with seven topic entries using the LEARN-01 template (Question / What we did in prototype / What's open for production CLI build / Severity):

| Anchor | Topic                                                                  | Severity |
| ------ | ---------------------------------------------------------------------- | -------- |
| CL-001 | JSON envelope vs raw-resource wire shape (gh/kubectl style)            | medium   |
| CL-002 | AGENT-04 timestamp format: RFC 3339 vs raw int64 millis pass-through   | low      |
| CL-003 | Phase 1 D-03 structured `reason` lift vs single `label` string         | medium   |
| CL-004 | Exit-code taxonomy richness (exit 1 + error.code vs distinct codes)    | low      |
| CL-005 | Watch-shaped use cases after --watch removal                           | high     |
| CL-006 | "Most recent" semantics (createdAt DESC vs most-recent-running)        | medium   |
| CL-007 | `--rollout-id` requiring `--environment` (PC-004 surface)              | low      |

Doc mirrors `.planning/API-PAPERCUTS.md` structure: header / tagline / last-updated / counts / Active Index table / Entries section / Resolved section.

## Deviations from Plan

**None — plan executed exactly as written.** All three tasks completed against the documented acceptance criteria. No Rule 1/2/3 auto-fixes were needed (the plan was thorough enough that the existing reference patterns in list.go / start.go / plaintext.go fully covered the implementation surface).

## API Papercut Candidates Discovered

**None new in this plan.** This was a CLI-side vertical slice against mocks — no real-staging traffic. New API papercuts (if any) will surface during Plan 03-02's real-staging smoke. Candidates flagged in the plan that may surface in 03-02:

- Empty-list response shape consistency (`{items: []}` vs `null` vs `{}`) — `resolveRollout` already handles both via the nil-guard + `len(items) == 0` check; if API returns yet another shape, papercut filed during 03-02.
- Get-by-env+rollout-id eventual consistency vs List — only observable against a real rollout that's mid-state-transition; 03-02 territory.
- Metric.status enum full set — only observable on a real guarded rollout with monitoring data; 03-02 territory.

## CLI-LEARNINGS Candidates Discovered During Implementation

A handful of small CLI/UX questions came up while writing the implementation. None are critical enough to seed as new CL-NNN entries during this plan — they're all variants of the seeded topics (CL-001 envelope, CL-003 reason lift, CL-006 most-recent). Listed here for the 03-02 smoke reviewer to consider whether they should become new entries after staging exposure:

- **Sectioned plaintext padding/columns:** The current renderer uses `text/tabwriter` for stages/metrics/events but fixed-spaced headers for the Overview block. Whether agents (and humans) want plaintext fully tabwriter'd, or only the structured blocks, is a CL-001 sub-question. Plan 03-02 smoke run on a real rollout will surface whether the formatting feels off.
- **Stage marker glyphs (`✓ → ` `):** Use UTF-8 box-drawing-ish marks. No fallback for non-UTF-8 terminals. Whether to ASCII-fallback (e.g., `[x] [>] [ ]`) is a CL-001-adjacent question if a real demo terminal renders garbage.
- **Terminal rollout stage rendering:** Per the renderer, terminal rollouts (completed/reverted) render all stages as `completed`. This is a small interpretation call — could also render the actual reached stage and mark the rest as `not-reached`. CL-006-adjacent.
- **Event timestamp format in plaintext:** Currently full RFC 3339. Plan 03-02 may surface whether agents prefer short forms (`HH:MMZ`) for readability vs full timestamps for machine consumption — relates to CL-002.

## Deliberate Scope Boundaries (Held)

- **D-01:** Zero `--watch` code, flags, tests, or polling helpers. Polling is the agent's job; the CLI is one-shot only.
- **D-12:** Zero new methods on `internal/rollouts.Client`. Phase 3 grows the surface by zero methods, honoring Phase 1 D-08.
- **D-11:** Zero structured-reason lift. `status.label` remains the only reason carrier in plaintext output.
- **Generic CLI robustness:** No new idempotency-key, exit-code taxonomy, retry shapes — status is read-only, kept simple.
- **JSON as API-passthrough:** Envelope reused verbatim from Phase 1. No new envelope fields, no Phase-3-specific classifiers. The bigger envelope-vs-raw question stays catalogued as CL-001 for the production CLI build to revisit.

## Self-Check: PASSED

- [x] `cmd/flags/rollouts/status.go` exists, contains NewStatusCmd, registers exactly the documented flag surface, contains Client.Get + Client.List call paths, contains NewRolloutEnvelope + NewErrorEnvelope + ErrCodeNoRolloutsFound references, contains D-03 validation guard before any client call.
- [x] `cmd/flags/rollouts/plaintext.go` contains `func RenderRolloutStatusPlaintext(r *rollouts.Rollout) string` and the renderer output for a non-nil rollout contains `Rollout:`, `Stages:`, `Metrics:`, `Events:` headers.
- [x] `cmd/flags/rollouts/rollouts.go` contains the literal call `cmd.AddCommand(NewStatusCmd(client))`.
- [x] `cmd/cliflags/flags.go` contains `RolloutIdFlag = "rollout-id"` and `RolloutIdFlagDescription` (mentioning `--environment` + PC-004), and `AllFlagsHelp` does NOT include RolloutIdFlag.
- [x] `internal/rollouts/errors.go` contains `ErrCodeNoRolloutsFound = "no_rollouts_found"`; no other ErrCode* constants renamed or removed.
- [x] `.planning/CLI-LEARNINGS.md` exists at the planning root with Active Index heading + exactly 7 `### CL-` subsection headings + each of the four LEARN-01 structured-template fields present 7 times each.
- [x] `go build ./...` exits 0.
- [x] `go test ./cmd/flags/rollouts/... -count=1` exits 0.
- [x] `go test ./internal/rollouts/... -count=1` exits 0.
- [x] `./ldcli flags rollouts-beta --help` lists `status` as a subcommand.
- [x] `./ldcli flags rollouts-beta status --help` advertises `--flag`, `--project`, `--environment`, `--rollout-id`.
- [x] All commits exist in git history: c273882, 8c0587e, 0985984.

## Next Up

Plan **03-02** (real-staging smoke) — run the new status command against `app.ld.catamorphic.com` / staging using a Phase 2 rollout, append any new API papercuts to `.planning/API-PAPERCUTS.md`, update Confluence page `4875452435` (fetch-first), and append any new CLI/UX learnings to `.planning/CLI-LEARNINGS.md`.
