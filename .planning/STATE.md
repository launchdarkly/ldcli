---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: planning
last_updated: "2026-05-12T20:46:27.519Z"
progress:
  total_phases: 4
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
---

# STATE: ldcli — Automated Rollouts via CLI

**Last updated:** 2026-05-12

## Project Reference

- **What this is:** A new `ldcli flags rollouts-beta` command surface for starting, monitoring, and managing automated releases (guarded + progressive rollouts) on top of LaunchDarkly's `automated-releases` API. First-class consumers: humans, CI/CD, and AI agents.
- **Core value:** An AI agent (or human, or CI/CD pipeline) can take a merged feature behind a flag, kick off an automated rollout, monitor it through to completion, and respond to regressions — without ever needing the LaunchDarkly UI.
- **Current focus:** Roadmap created (4 phases, coarse granularity, MVP mode). Ready for `/gsd-plan-phase 1`.

## Current Position

- **Phase:** 0 → 1 (roadmap complete; planning Phase 1 next)
- **Plan:** None yet
- **Status:** Ready to plan Phase 1
- **Progress:** `░░░░░░░░░░` 0 / 4 phases complete

## Roadmap Summary

| # | Phase | Status | Plans |
|---|-------|--------|-------|
| 1 | List (foundation + first end-to-end slice) | Not started | TBD |
| 2 | Start a rollout | Not started | TBD |
| 3 | Status & Watch | Not started | TBD |
| 4 | Stop, Dismiss, & Finalize papercuts | Not started | TBD |

## Performance Metrics

Not yet measured. Targets to track once Phase 1 plans land:

- `start` end-to-end latency (PATCH + re-fetch): target <3s success path.
- Preflight check duration: target <2s (parallelized with `errgroup`).
- `--watch` poll cadence: 15s default, clamp minimum 5s; exponential backoff to 60s.

## Accumulated Context

### Decisions Made (in PROJECT.md / REQUIREMENTS.md / Research)

| Decision | Source | Outcome |
|---------|--------|---------|
| Hand-roll types in `internal/rollouts/`; do NOT add `automated-releases` paths to `ld-openapi.json` | Architecture research | Pending Phase 1 |
| Mutations via existing public flag semantic-patch endpoint (`startAutomatedRelease`, `stopAutomatedRelease` instructions) | Architecture research | Pending Phase 2 / Phase 4 |
| Observability via direct REST to `/internal/projects/.../automated-releases/...` (account-token auth works) | Architecture research | Pending Phase 1 / Phase 3 |
| Two-step `start` = PATCH + follow-up GET (rollout ID not returned by mutation) | Architecture research P1 | Pending Phase 2 |
| `recommended-duration` is the preflight proxy (no dedicated validate endpoint exists) | Architecture research P8 | Pending Phase 2 |
| Versioned JSON envelope `schemaVersion: "rollouts.v1beta1"` + `kind` + `data` + `meta` shared by every command | Stack + Features research | Pending Phase 1 |
| Exit-code taxonomy aligned-but-simpler than sysexits; reconciled in Phase 1 | Stack + Features research | Pending Phase 1 |
| `go-retryablehttp@v0.7.7` for retries; `google/uuid` (already vendored) for `Idempotency-Key`; `golang.org/x/term` for TTY (no `mattn/go-isatty`) | Stack research | Pending Phase 1 |
| Watch is `gh pr checks --watch` style (alt screen + simple redraw); NDJSON when `--output json`; explicitly NOT Bubbletea | Stack research | Pending Phase 3 |
| Watch defaults to "until next actionable event," not "until terminal," for multi-day rollouts | Pitfalls research #6 | Pending Phase 3 |
| Diff-based transition detection in watch, not status-only polling | Pitfalls research #5 | Pending Phase 3 |
| `stop --to-variation` is required (no implicit default); covers both original and target directions | Features research | Pending Phase 4 |
| `-beta` command suffix carries forward; breaking changes acceptable within `rollouts-beta` tree | PROJECT.md | Locked |
| Reuse existing ldcli auth (OAuth + access tokens); no new auth surface | PROJECT.md | Locked |
| `.planning/API-PAPERCUTS.md` is a first-class milestone deliverable, seeded with 16 papercuts from architecture research | PROJECT.md + Architecture research | Pending Phase 1 (seed) → Phase 4 (review) |

### Open Questions / Spike Items

To be answered during planning or implementation, not blocking roadmap creation:

- **`Idempotency-Key` honored by upstream?** Stack research recommends sending; architecture research notes "unverified whether gonfalon honors it for deduplication." Validate in Phase 2 spike; document outcome in papercuts doc.
- **`recommended-duration` preflight detail granularity?** Per-metric pass/fail or aggregate only? Needs staging validation in Phase 2 before health-check UX is finalized.
- **`waiting` status enum semantics?** Undocumented per architecture research P6. Default Phase 3 watch behavior: treat as non-terminal; surface to user; revisit if behavior is wrong.
- **`dismiss_regression` eventual-consistency window?** Architecture Anti-Pattern 3 suggests 1s/3s backoff with ~10s timeout. Empirically measure during Phase 4.
- **Exit code numbering reconciliation.** Stack research proposes sysexits-aligned (64, 65, 69, 75, 77); Features research proposes sequential (0–9, 70, 75). Roadmap leaves to Phase 1 plan to lock — recommend sequential set for ergonomics with sysexits commentary inline.

### Todos

None yet. To be populated by `/gsd-plan-phase 1`.

### Blockers

None.

## Session Continuity

- **Next command:** `/gsd-plan-phase 1`
- **Files to re-read at session start:**
  - `.planning/ROADMAP.md`
  - `.planning/REQUIREMENTS.md`
  - `.planning/PROJECT.md`
  - `.planning/research/SUMMARY.md`
  - `.planning/research/ARCHITECTURE.md` (Phase 1 needs the API inventory + papercut seeds)
  - `.planning/codebase/ARCHITECTURE.md` and `STRUCTURE.md` (existing patterns)
- **Working tree:** Clean.

---
*State initialized at roadmap creation: 2026-05-12*
