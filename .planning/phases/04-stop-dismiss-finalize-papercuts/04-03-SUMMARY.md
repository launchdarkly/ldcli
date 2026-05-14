---
phase: 04-stop-dismiss-finalize-papercuts
plan: 03
subsystem: rollouts-beta
tags: [smoke, real-staging, stop, dismiss-regression, papercuts, prototype]
requires:
  - 04-01 stop command shipped (cmd/flags/rollouts/stop.go + internal/rollouts/stop.go)
  - 04-02 dismiss-regression shipped (cmd/flags/rollouts/dismiss.go + internal/rollouts/dismiss.go)
  - LD staging access token loaded via ~/.config/ldcli/config.yml
  - Confluence MCP available (mcp__mcp-atlassian__confluence_get_page/update_page)
provides:
  - .planning/phases/04-stop-dismiss-finalize-papercuts/04-SMOKE.md (7 captured scenarios + Plan 04-02 open-questions answers section)
  - API-PAPERCUTS.md PC-021 (Status.Kind taxonomy gap)
  - Confluence page_id 4875452435 v5 (PC-021 synced as entry 3)
  - CLI-LEARNINGS.md CL-013 (dismiss pre-read gates on wrong field)
  - CLI-LEARNINGS.md CL-014 (stop --to-variation accepts any UUID)
  - CLI-LEARNINGS.md CL-015 (meta.uiURL is flag-level, not rollout-level)
  - CLI-LEARNINGS.md CL-008/CL-009 Phase 4 confirmation appends
  - cmd/flags/rollouts/dismiss.go: // PAPERCUT: PC-021 annotation at the no-active-regression pre-read
affects:
  - .planning/API-PAPERCUTS.md (Active Index + Entries + Active count 20→21)
  - .planning/CLI-LEARNINGS.md (Active Index + Entries + Active count 12→15)
  - Confluence page 4875452435 (entry 3 added; v4→v5)
tech-stack:
  added: []
  patterns:
    - real-staging smoke via the same fixture flags used in Phases 1–3 (alex-engelberg-dev / test)
    - token-redaction discipline carried forward from prior SMOKE.md files (no literal token bytes; verified via grep gate)
    - fetch-first Confluence pattern (memory feedback_confluence_fetch_first.md) honored
key-files:
  created:
    - .planning/phases/04-stop-dismiss-finalize-papercuts/04-SMOKE.md
    - .planning/phases/04-stop-dismiss-finalize-papercuts/04-03-SUMMARY.md
  modified:
    - .planning/API-PAPERCUTS.md
    - .planning/CLI-LEARNINGS.md
    - cmd/flags/rollouts/dismiss.go
decisions:
  - D-04-03-01: 7 smoke scenarios captured (5 mandatory A–E + plaintext sanity G + skipped F). Smoke F skipped because no Status.Kind="regressed" fixture exists upstream — recorded as "could not reproduce empirically" per plan acceptance criteria.
  - D-04-03-02: PC-021 filed as a new contract-shape papercut (taxonomy omits "regressed"). Synced to Confluence (DOC-04). Source code annotated with // PAPERCUT: PC-021 at the dismiss pre-read.
  - D-04-03-03: CL-013 (new, high severity) carries the production-CLI guidance for reshaping the dismiss pre-read; cross-references PC-021. Production CLI build must address before shipping dismiss-regression.
  - D-04-03-04: CL-014 + CL-015 surfaced as net-new prototype-era observations (stop variation validation, UI URL anchor precision). Severity medium and low respectively.
  - D-04-03-05: Plan 04-02 open questions #1 (polling-budget) and #2 (instruction body shape) marked unanswered empirically — blocked behind the PC-021/CL-013 gap. Re-run after the pre-read is reshaped will close them.
  - D-04-03-06: Plan 04-02 open question #4 (BuildUIURL path shape) answered — flag-level URL resolves correctly; rollout-level anchor is a CL-015 production-CLI candidate.
metrics:
  duration_minutes: ~40
  completed_date: 2026-05-14
  commits: 2
  smoke_scenarios:
    captured: 7
    passed: 7
    skipped: 1   # Smoke F bounded-backoff timeout
  papercuts_filed: 1   # PC-021
  cli_learnings_added: 3   # CL-013, CL-014, CL-015
  cli_learnings_amended: 2   # CL-008, CL-009 Phase 4 confirmations
  confluence_updates: 1   # page 4875452435 v4→v5
  plan_04_02_open_questions_answered: 1   # of 4 (#4); #1, #2 blocked behind PC-021; #3 inverted finding
---

# Plan 04-03 SUMMARY — Real-staging smoke for stop + dismiss-regression

## What shipped

`.planning/phases/04-stop-dismiss-finalize-papercuts/04-SMOKE.md` — a 357-line real-staging exercise log capturing 7 smoke scenarios (A–E mandatory, F skipped with reason, G plaintext sanity) covering both `stop` and `dismiss-regression` end-to-end against `alex-engelberg-dev` / `test` on `https://ld-stg.launchdarkly.com`. Token redaction discipline holds (grep gate returns nothing). Plan 04-02's four open questions are addressed explicitly in a dedicated "Plan 04-02 open questions answered" section.

## Smoke summary

| Smoke | Command | Exit | Status.Kind | Verdict |
|-------|---------|------|-------------|---------|
| A | stop → target variation (roll forward) | 0 | completed | ✓ Pass; ~3s latency; meta.uiURL resolves |
| B | stop → original variation (roll back) | 0 | reverted | ✓ Pass; ~1s latency |
| C | re-run stop on terminal rollout | 1 | (refused) | ✓ Pass; error.code=rollout_already_terminal |
| D | dismiss against paused-with-regression | 1 | (refused at state "paused") | ⚠ Reveals design gap — PC-021 / CL-013 |
| E | dismiss against active rollout | 1 | (refused at state "active") | ✓ Pass; error.code=no_active_regression |
| F | bounded-backoff timeout | — | — | ⊘ Skipped: no `regressed` fixture exists upstream |
| G | plaintext sanity (FORCE_TTY=1) | 0 | completed | ✓ Pass; concise 3-line renderer output |

## The big finding

Phase 4's main empirical discovery is **PC-021 / CL-013**: the upstream `Status.Kind` taxonomy does not include `"regressed"`. Across 12 rollouts in 5 flags surveyed, the kinds seen were `{paused, reverted, completed, active}` — `"regressed"` never appeared. Guarded rollouts that have hit a regression surface as `Status.Kind == "paused"` with the regression encoded in `status.label`. The dismiss-regression command's pre-read (`if current.Status.Kind != "regressed"`) therefore rejects every real regression scenario on staging — the bounded-backoff polling loop and the PC-007 timeout warning path were not exercisable against real staging without reshaping the gate.

This is a real prototype-era learning, not a bug in Plan 04-02's code. The implementation faithfully follows the architecture research's assumed contract; the contract turned out to be different in practice. PC-021 (filed) puts the question to the API team; CL-013 (filed) puts the question to the production CLI build owner. The dismiss code is annotated with `// PAPERCUT: PC-021` so the next reader sees the gap immediately.

## Plan 04-02 open questions — final status

1. **Polling budget rightness (1s/3s/5s, ~9s):** **Unanswered empirically.** The pre-read refused every dismiss attempt; the polling loop never fired. Re-run is blocked behind PC-021/CL-013.
2. **dismissRegression instruction body shape:** **Unanswered empirically.** No PATCH was sent. Same blocker.
3. **Post-dismiss Status.Kind:** **Inverted finding.** No pre-dismiss "regressed" Kind exists upstream. The "post-dismiss" question is moot until the pre-read is reshaped. **Adjacent finding (stop case):** stop → target → `completed`; stop → original → `reverted`.
4. **BuildUIURL path shape:** **Answered (flag-level, with caveat).** All captured uiURLs resolve in the LD UI. Path is `/{project}/{env}/features/{flagKey}/targeting` — flag-level, not rollout-level. CL-015 carries the production-CLI follow-up for a more precise anchor.

## Net new artifacts

| Artifact | Type | Severity | Where |
|----------|------|----------|-------|
| **PC-021** | API papercut | n/a | `.planning/API-PAPERCUTS.md` + Confluence page_id 4875452435 v5 + `// PAPERCUT: PC-021` annotation in `cmd/flags/rollouts/dismiss.go` |
| **CL-013** | CLI/UX learning | high | `.planning/CLI-LEARNINGS.md` |
| **CL-014** | CLI/UX learning | medium | `.planning/CLI-LEARNINGS.md` |
| **CL-015** | CLI/UX learning | low | `.planning/CLI-LEARNINGS.md` |
| **CL-008 + CL-009 Phase 4 confirmations** | CLI/UX learning amendments | — | `.planning/CLI-LEARNINGS.md` |

## Token redaction holds

`grep -cE '[a-f0-9]{40,}' .planning/phases/04-stop-dismiss-finalize-papercuts/04-SMOKE.md` returns `0`.
`grep -c 'api-4af2' .planning/phases/04-stop-dismiss-finalize-papercuts/04-SMOKE.md` returns `0`.

## Phase 4 readiness for milestone close (Plan 04-04)

**Phase 4 is ready for milestone review.** All four Phase 4 plans have shipped working code or recorded findings:

- 04-01 (stop command vertical slice): code + 7 tests + 04-01-SUMMARY.md
- 04-02 (dismiss-regression vertical slice): code + 7 tests + 04-02-SUMMARY.md (carries the four open questions answered here)
- 04-03 (real-staging smoke): 7 scenarios captured + 1 new papercut filed + 3 new CL entries + 2 CL amendments + Confluence sync
- 04-04 (milestone close): cannot start until 04-03's appends land — now satisfied.

Plan 04-04 should review the now-21-entry API-PAPERCUTS.md and 15-entry CLI-LEARNINGS.md for accuracy, sync Confluence one final time (probably no-op since PC-021 just landed), and circulate both artifacts to their target audiences (API team for papercuts, production CLI build owner for learnings) per SC#5.
