# ldcli — Automated Rollouts via CLI

## What This Is

ldcli is LaunchDarkly's official Go CLI for managing feature flags, environments, members, and a local dev server. This milestone adds a new `ldcli flags rollouts-beta` command surface for **starting, monitoring, and managing automated releases** (guarded + progressive rollouts) — designed so humans, CI/CD pipelines, and AI agents can safely ship features end-to-end behind a flag without leaving the terminal.

## Core Value

An AI agent (or human, or CI/CD pipeline) can take a merged feature behind a flag, kick off an automated rollout, monitor it through to completion, and respond to regressions — without ever needing the LaunchDarkly UI.

## Requirements

### Validated

<!-- Existing capabilities of ldcli, inferred from the codebase map. -->

- ✓ Manage flags, environments, members, and other LD resources via CLI commands — existing
- ✓ OpenAPI-generated resource commands kept in sync with the LaunchDarkly API — existing
- ✓ Local dev server with embedded React UI and SQLite storage — existing
- ✓ Authentication (login + config) via OAuth and access tokens — existing
- ✓ JSON / plaintext output formatting — existing
- ✓ Multi-channel distribution (Homebrew, Docker, NPM, GitHub Releases) — existing
- ✓ Analytics instrumentation via `PersistentPreRun` hooks — existing
- ✓ **REQ-START-01** — Start an automated rollout via `ldcli flags rollouts-beta start` (progressive default; guarded when `--pause-on-regression`/`--revert-on-regression` supplied) — validated in Phase 2
- ✓ **REQ-START-02** — Configure stages, metrics, randomization unit, target/original variation, auto-rollback behavior from CLI flags — validated in Phase 2 (rule/clauses + extension-duration deferred per D-07/Q5)
- ✓ **REQ-START-03** — Target any environment via `--environment <key>` — validated in Phase 2

### Active

<!-- New scope for this milestone. -->

- [ ] **REQ-START-04** — Default to **erroring** (in non-interactive contexts) or **prompting** (interactively) when metric health checks fail; bypass with `--skip-health-checks`. Prevents agents from launching rollouts with mis-instrumented metrics. **[Preflight deferred to a future phase per D-09]**
- [ ] **REQ-LIST-01** — List all rollouts on a flag — both currently-running and past.
- [ ] **REQ-STATUS-01** — Easily get the status of the most recent rollout on a flag (running now, or the last one that completed/failed).
- [ ] **REQ-STATUS-02** — Show all the information about a rollout that's currently visible in the UI: percentage stages, latest metric results, current stage, monitoring state.
- [ ] **REQ-STATUS-03** — Provide a `--watch` mode (modeled on `gh pr checks --watch`) that surfaces **actionable** events (regressions, action-required transitions) rather than only terminal states.
- [ ] **REQ-STOP-01** — Manually stop a rollout, with the operator choosing whether to roll out to the control (original) or test (target) variation.
- [ ] **REQ-DISMISS-01** — Manually dismiss a regression so the rollout can continue.
- [ ] **REQ-UX-01** — Terminology and language for rollout statuses should be consistent with what the LaunchDarkly UI shows today (nice-to-have, when it makes sense).
- [ ] **REQ-AGENT-01** — Commands produce machine-readable output (JSON option) and meaningful exit codes so AI agents and CI/CD can chain decisions safely.
- [ ] **REQ-DOC-01** — Maintain a running **API papercuts** document (`.planning/API-PAPERCUTS.md`) capturing confusing or high-friction parts of the `automated-releases` API, with suggested improvements for the API team. This is a first-class deliverable of the milestone.

### Out of Scope

- **Timeseries / chart data for metric results** — the UI shows these; the CLI surfaces latest values only.
- **Release-policy-driven defaults** — future work. Today, every command takes explicit options. Once release policies are GA, the CLI may allow `start` with no options when a policy provides them.
- **Configuring metric definitions or randomization units** — these are pre-existing LD resources; the CLI consumes them, it doesn't create them.
- **"Notify human" as an explicit command** — escalation is the agent's choice (e.g., open an issue, ping Slack); the CLI itself doesn't ship a notification primitive.

## Context

- **Brownfield project.** ldcli is already a mature, distributed CLI (Cobra/Viper, OpenAPI-generated resource commands, embedded React dev server, multi-channel distribution). This milestone adds a new subcommand surface that follows existing patterns.
- **The `automated-releases` API is the unified successor** to two earlier APIs in gonfalon (`measured-rollouts`, `progressive-rollouts`). It's currently **undocumented and unstable**. This CLI work will be the **first real consumer**, which is why surfacing papercuts is a milestone deliverable.
- **Key gonfalon entrypoints** (to be validated by the researcher subagent during research phase):
  - `startAutomatedRelease` flag patch instruction
  - `stopAutomatedRelease` flag patch instruction
  - REST endpoints under `/automated-releases/`
- **AI-agent use cases are first-class.** The CLI must produce predictable, deterministic, machine-readable output so an autonomous agent can decide whether to wait, escalate, dismiss a regression, or roll back.
- **The `-beta` suffix on the command name** is an intentional signal to users (and agents) that this surface may change as the underlying API evolves.

## Constraints

- **Tech stack**: Must integrate with existing ldcli architecture — Cobra subcommands, the `internal/` `Client` interface pattern for testability, JSON/plaintext output formatting, and the OpenAPI-driven resource command generator where applicable.
- **API stability**: The `automated-releases` API is unstable. Work around issues as they come up; document papercuts; don't block on upstream API fixes.
- **Beta surface**: The `-beta` suffix carries forward; breaking changes are acceptable within this command tree.
- **Backwards compatibility**: Must not break any existing ldcli command, distribution channel, or analytics behavior.
- **Authentication**: Reuse existing ldcli auth (OAuth + access tokens via `ldcli config`); no new auth surface.
- **Real-server validation**: Before declaring a phase complete, the executor must exercise the new command surface against a real LaunchDarkly instance (staging or prod) with real credentials and confirm the happy path returns the expected envelope. If this isn't possible (e.g. unstable API outage), the executor must explicitly call that out in SUMMARY.md rather than silently skip.
- **API contract learnings → Confluence**: When working against the real automated-releases API, any *contract-shape* observation — confusing field names, missing data on responses, forced consumer workarounds, inconsistencies with the rest of the LD API surface — must be captured in the Confluence doc **[Learnings: automated release API papercuts](https://launchdarkly.atlassian.net/wiki/spaces/~62435d09f6a26900695be8d7/pages/4875452435)** (`page_id=4875452435`). Scope is the **API contract**, not our CLI's bugs. **Always fetch the page before updating** (`mcp__mcp-atlassian__confluence_get_page` then `confluence_update_page`) so concurrent human edits aren't clobbered. The on-disk `.planning/API-PAPERCUTS.md` is a complementary doc for source-code-anchored workarounds (`// PAPERCUT: PC-NNN`) — Confluence is the human-readable feedback channel for the API team.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Unified `start` command (progressive default, guardrail metrics opt-in) | Simpler mental model; aligns with the API's evolution toward unification | — Pending |
| `gh pr checks` style status + `--watch` for actionable events | Familiar UX; agents shouldn't watch multi-day rollouts continuously, but should react to regressions | — Pending |
| Default-fail on metric health-check problems; `--skip-health-checks` to override | Protects agents from launching rollouts on mis-instrumented metrics | — Pending |
| Maintain `.planning/API-PAPERCUTS.md` as a milestone deliverable | First-consumer feedback is the highest-leverage input to the API team before public release | — Pending |
| Target any environment via parameter (no env-promotion workflow) | Keeps v1 scope tight; cross-env workflows can be composed by callers | — Pending |
| Configure + start in one command | Matches user intent; pre-existing-config-only is a less useful subset | — Pending |
| `-beta` command suffix | Signals instability; allows breaking changes as the underlying API stabilizes | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-13 after Phase 2 completion (start-a-rollout vertical slice)*
