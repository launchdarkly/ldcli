# Phase 3: Status & Watch - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-14
**Phase:** 3-Status & Watch
**Areas discussed:** Reconcile with Phase 1 contract, Watch design (then scrapped mid-discussion), Status output shape, Most-recent resolution, D-03 reason lift, JSON envelope (then scrapped mid-discussion), Project framing pivot to prototype-first

---

## Reconcile with Phase 1 contract

### Q1: ROADMAP SC#2 bucketed `state` vocabulary vs Phase 1 D-02 `kind`

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse `kind` (Recommended) | Honor Phase 1 D-02 unchanged. Treat SC#2's vocabulary as informal early-draft labels. | ✓ |
| Add parallel `state` field | Keep `kind` for list parity; emit a NEW `status.state` field in the SC#2 vocabulary alongside `kind`. | |
| Rename kind → state, rebucket | Reframe Phase 1 D-02. Touches every renderer/test from Phase 1. | |

**User's choice:** Reuse `kind`.
**Notes:** SC#2's informal vocabulary maps to D-02 kinds (running≈active, succeeded≈completed, failed≈reverted, regression-detected=regressed, paused=paused).

### Q2: Watch-timeout exit code

| Option | Description | Selected |
|--------|-------------|----------|
| Exit 0 + outcome in terminal record (Recommended) | Treat watch-timeout as graceful success. | |
| Exit 1, distinguish via error.code only | Strictest D-01 reading; `error.code: "watch_timeout"`. | ✓ |
| Exit 124 (GNU timeout convention) | Carve out a single numeric exit code matching GNU coreutils. | |

**User's choice:** Exit 1, distinguish via error.code only.
**Notes:** Became moot when `--watch` was scrapped in a later turn.

### Q3: Terminal-state exit code under --watch

| Option | Description | Selected |
|--------|-------------|----------|
| Exit 0 for any terminal state (Recommended) | Watch session did its job; rollout outcome ≠ CLI error. | ✓ |
| Exit 0 only when kind=completed; exit 1 for reverted/paused-terminal | Treat auto-rolled-back / SRM-stopped as CLI-layer errors. | |
| Always exit 0 when terminal:true | Same as option 1 but more explicit. | |

**User's choice:** Exit 0 for any terminal state.
**Notes:** Also moot after `--watch` removal.

### Q4: Update ROADMAP/REQUIREMENTS, or document divergence only

| Option | Description | Selected |
|--------|-------------|----------|
| Update ROADMAP + REQUIREMENTS via /gsd-phase follow-up (Recommended) | Mirror Phase 2's pattern. | ✓ |
| Document divergence in CONTEXT.md only | Lower-cost; risk of stale ROADMAP. | |
| Both — doc + automatic ROADMAP rewrite | Higher cost; clobber risk. | |

**User's choice:** Update ROADMAP + REQUIREMENTS via /gsd-phase follow-up.
**Notes:** Now expanded to cover full `--watch` removal (strike STATUS-05..09 + ROADMAP SC#3, #4, #5) plus SC#2 wording softening.

---

## Watch design (sketched-and-scrapped)

Claude sketched a complete `--watch` design at the user's request: poll loop with diff-based event derivation, NDJSON event-per-line envelope shape with `meta.watchSequence` + `meta.terminal: true`, a closed `eventType` enum (watch_started / stage_advanced / regression_detected / srm_detected / action_required / monitoring_extended / dismissal_observed / terminal), `gh pr checks --watch`-style human-mode TTY redraw, 15s default interval clamped [5s, 5m], 4h default timeout.

**User's response:** "I've decided watch is too complicated and I don't want it to be in scope for this project. only implement a way of getting the current status, with the idea that an agent can keep polling that periodically."

**Effect:** `--watch` and all of STATUS-05..09 are removed from Phase 3 and the project. Polling becomes the agent's responsibility.

---

## Status output shape (post-watch-removal)

### Q5: Plaintext layout for `status <flag>`

| Option | Description | Selected |
|--------|-------------|----------|
| Sectioned blocks (Recommended) | Overview / Stages / Metrics / Events sections; mirrors LD UI's information architecture. | ✓ |
| Flat key:value (mirrors list --detailed) | Reuse renderDetailed-style key:value layout. | |
| Sectioned for default, --short for terse | Sectioned by default; add `--short` for scripting. | |

**User's choice:** Sectioned blocks.
**Notes:** Full detail by default; no `--detailed` toggle in v1 (status is already a single rollout).

### Q6 (asked-and-clarified): JSON envelope `meta` fields

Claude proposed three options (minimal `fetchedAt` / +uiURL / +uiURL+availableActions). User declined to answer and asked for clarification on what "envelope" meant, then on whether envelopes are typical for CLI JSON output.

**Resolution:** Claude clarified that the envelope is a Phase 1 design choice (not a CLI norm; `gh`/`kubectl`/`aws` use raw-resource shapes). User initially said "I think the envelope is kinda stupid, I don't want that. can we go back on that?" Claude laid out the cost of nuking it. User then pivoted: "let's simplify the approach. don't worry about nuking the envelope."

**Effect:** Envelope is kept verbatim from Phase 1. The bigger question (envelope vs raw-resource) is captured as a CLI-LEARNINGS.md topic for the production CLI build.

---

## Most-recent resolution (Claude's call, prototype-fast)

Two sub-decisions Claude made under the prototype-first framing (no user question):

- **With `--rollout-id`:** require `--environment` (validator-reject). Rationale: PC-004 (GET-by-ID requires env in URL path). Auto-resolving env via list-and-filter is a CLI-side workaround that papers over the API gap — leave it bare so the gap is felt by demo consumers.
- **Without `--rollout-id`:** `Client.List(flag, env=optional, limit=1)` → items[0]. Most recent by `createdAt DESC` (Phase 1's existing sort). Empty list → `error.code: "no_rollouts_found"`.

---

## D-03 structured `reason` lift (Claude's call, prototype-fast)

**Claude's call:** Don't lift in Phase 3. Stay with `status.label` only. Whether agents struggled with `label` parsing is a CLI-LEARNINGS topic for the production build.

---

## Claude's Discretion

- File split (`status.go`, `RenderRolloutStatusPlaintext` in `plaintext.go`)
- Section ordering, field-order within sections, ANSI color use
- Whether CLI-LEARNINGS.md is seeded in the first plan task or populated lazily
- Help-text wording (especially around `--rollout-id` + `--environment` pairing)

---

## Deferred Ideas

- **`--watch` revisit** — out of project scope; future milestone candidate if demo feedback shows polling pain
- **Phase 1 D-03 structured `reason`** — deferred further; revisit in production CLI build
- **JSON envelope vs raw-resource wire contract** — CLI-LEARNINGS.md topic; production build revisits
- **`uiURL` / `availableActions` in `meta`** — rejected for Phase 3 (one smuggles watch-lite, the other duplicates state)
- **`--detailed` / `--short` plaintext toggles** — deferred; not needed for prototype
- **AGENT-04 (RFC 3339 + duration strings in JSON)** — kept Phase 1 behavior; "should JSON be raw int64 millis passthrough?" is a CLI-LEARNINGS topic
