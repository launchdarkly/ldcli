# Phase 2: Start a rollout - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-13
**Phase:** 02-start-a-rollout
**Areas discussed:** Stages flag UX, Metrics + auto-rollback shape, Targeting scope in v1, Preflight UX & audit, Idempotency-Key

---

## Stages flag UX

### Q1 — Stage flag syntax

| Option | Description | Selected |
|--------|-------------|----------|
| Compact list `--stages 25:60m,50:60m,100:60m` | Colon-separated allocation:duration pairs, comma-delimited. Matches research proposal. | ✓ |
| Repeatable `--stage 25:60m --stage 50:60m` | One stage per flag. Cleaner for very long lists; verbose for short ones. | |
| External JSON `--stages-file stages.json` | Full API field set, future-proof. Hostile for ad-hoc agent invocations. | |
| Both compact + file | Ship `--stages` (compact) AND `--stages-file`; error if both set. More surface to test. | |

**User's choice:** Compact list.

### Q2 — Allocation unit

| Option | Description | Selected |
|--------|-------------|----------|
| Percent (0-100) | `25:60m` means 25%; CLI multiplies by 1000 for API basis-points. Decimals rejected. | ✓ |
| Basis points (0-100000) | `25000:60m` means 25%. Exact API passthrough; cognitively brutal. | |
| Suffixed | `25%:60m` or `25000bp:60m`. Verbose; users still get it wrong. | |

**User's choice:** Percent ints.

### Q3 — Duration parsing

| Option | Description | Selected |
|--------|-------------|----------|
| Go duration strings | `60m`, `1h30m`, `2h`, `300s`. `time.ParseDuration`. | ✓ |
| Millis only | `3600000:25`. Exact passthrough; hostile. | |
| Both, autodetect | Magic; surprise mode. | |

**User's choice:** Go duration strings.

### Q4 — Single-stage shorthand

| Option | Description | Selected |
|--------|-------------|----------|
| No shorthand | Single stage = `--stages 50:60m`. One way to do it. | ✓ |
| `--target-allocation` + `--duration` | Two scalar flags for single-stage case. More surface. | |

**User's choice:** No shorthand.

---

## Metrics + auto-rollback shape

### Q1 — Metric flag shape

| Option | Description | Selected |
|--------|-------------|----------|
| Repeatable `--metric` | Each metric one flag. Familiar from `kubectl argo rollouts`. | ✓ (initial) |
| Comma list `--metrics a,b` | Compact; harder to extend per-metric without escaping. | |
| Pair-syntax `--metric key:opt` | Reserve `:` for per-metric options. Collides with stages syntax. | |

**User's choice:** Initially `--metric` repeatable, then the user proposed a much better alternative which superseded this question — see Q2 follow-up.

### Q2 — User-proposed alternative (adopted)

The user proposed splitting auto-rollback into two verb-style repeatable flags:

```
rollouts-beta start \
  --pause-on-regression metric1 \
  --revert-on-regression metric2
```

Claude's evaluation: clearer than the original proposal because (a) flag names describe what happens on regression rather than embedding an `autoRollback` boolean; (b) eliminates `--metric` entirely (the monitored set is the union of the two flags), sidestepping the PC-010 parallel-list sync trap; (c) rides Phase 1's `paused`/`reverted` vocabulary (D-02).

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, lock it | Drop `--metric`. Pause/revert pair is the only metric-declaration surface. | ✓ |
| Keep `--metric` too | Three flags; plain `--metric` defaults to pause; verb flags override. More surface; confusion when both passed for same key. | |

**User's choice:** Lock it.

### Q3 — Metric groups (`isGroup: true`)

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror the pair | `--pause-on-regression-group` / `--revert-on-regression-group`. Verbose but symmetric. | |
| Defer to v1.1 | v1 supports metrics only. Smaller surface. | ✓ |
| Prefix on existing flags | `--pause-on-regression group:my-group`. Compact; collision risk. | |

**User's choice:** Defer to v1.1.

### Q4 — `--release-kind` flag

| Option | Description | Selected |
|--------|-------------|----------|
| Error fast on conflict | Mutually exclusive: `--release-kind progressive` + pause/revert flag = usage error. | |
| Drop `--release-kind` entirely | Pure inference: zero pause/revert flags → progressive; ≥1 → guarded. Smallest surface. | ✓ |
| Silently coerce to guarded | Presence of any pause/revert flag promotes to guarded. Surprising. | |

**User's choice:** Drop entirely.

---

## Targeting scope in v1

### Q1 — Which targeting modes

| Option | Description | Selected |
|--------|-------------|----------|
| Fallthrough + existing rule | No flag → fallthrough; `--rule-id`/`--ref` → existing rule. New-rule deferred. | ✓ |
| Fallthrough only | v1 = default rule only. Smallest surface. | |
| All three modes | Add `--clauses` + `--rule-description` + `--before-rule-id`. Complex; agents struggle with Clause JSON. | |

**User's choice:** Fallthrough + existing rule.

### Q2 — Selector for existing-rule

| Option | Description | Selected |
|--------|-------------|----------|
| Both `--rule-id` and `--ref` | Mutually exclusive at CLI; mirrors API. | |
| `--rule-id` only | User added rationale: "`--ref` is only necessary when you're adding a rule and starting a rollout on the rule in a single patch" — which Phase 2 isn't doing. | ✓ |
| Single `--rule` that accepts either | Heuristic UUID vs string. Magic; ambiguous. | |

**User's choice:** `--rule-id` only.

**User's notes:** `--ref` is only useful when the user is creating a rule AND starting a rollout on it in a single semantic-patch batch. Phase 2 is start-only, so the rule must already exist, which means the operator has the ruleId. Revisit `--ref` if a future phase adds multi-instruction patches.

### Q3 — Default-rule dogfood error

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated `error.code` | `"default_rule_disabled"` + `nextAction` hint. | |
| Generic upstream error | `"unknown_upstream"` + verbatim server message. No special-case. | ✓ |

**User's choice:** Generic upstream error.

---

## Preflight UX & audit

**Outcome:** The user dropped the entire preflight concept from Phase 2.

User's stated rationale: "I think the whole preflight concept is overcomplicating this. I do want to build this, but it should be a later phase of this project."

No questions were answered in this area; the area was retired mid-discussion. START-04 and Phase 2 Success Criterion #3 will move to a future Preflight phase within this project (NOT struck — preflight is a domain-specific feature of automated releases).

---

## Idempotency-Key

**Origin:** Surfaced when Claude offered Idempotency-Key as one of three remaining open areas. User asked: "why are we caring about an idempotency key? is that something other cli features currently support?"

**Claude's response (honest answer):** No, no other ldcli command sends `Idempotency-Key`. The header arrived in REQUIREMENTS via Stack research as a defensive-hygiene recommendation; the gonfalon `automated-releases` API does not document support; the `go-retryablehttp` layer plus server-side "Flag must not have ongoing rollout" guard handle retry safety without it.

### Q1 — Scope

| Option | Description | Selected |
|--------|-------------|----------|
| Send header, no CLI flag | Auto-UUID per mutation; echoed in `meta.idempotencyKey`. No `--idempotency-key` user flag. | |
| Drop entirely from Phase 2 | No header, no flag, no echo. Requires REQUIREMENTS edit. Cleanest. | ✓ |
| Full `--idempotency-key` flag | Auto-default + user override. Maximum surface. | |

**User's choice:** Drop entirely from Phase 2.

**Follow-up user feedback (saved to memory as project-wide preference):** "In general, I don't really want to think about cli-robustness features that are general to mutative CLI commands (errors, idempotency) but we haven't already adopted in other parts of the CLI. that should be tackled in a general way outside of this project."

This elevates the idempotency drop from "deferred to a later phase" to "out of scope for the entire rollouts project" and updates the REQUIREMENTS follow-up to recommend striking START-06 / FOUND-05 idempotency clauses / AGENT-03 idempotency clauses from REQUIREMENTS.md (not moving them).

---

## Claude's Discretion

Items the user did not explicitly decide and that the planner is free to resolve:

- Whether to expose `--comment` for the semantic-patch envelope.
- Re-fetch robustness specifics (eventual-consistency backoff, stale-detection, behavior when GET returns empty after successful PATCH).
- Error-code taxonomy for the new mutation failure modes (flag-off, rollout-already-running, invalid-variation, auth-scope) — map API errors to codes; do NOT pre-fetch flag state.
- Whether `--target-variation` / `--original-variation` accept variation keys, IDs, or either.
- File split inside `internal/rollouts/` and `cmd/flags/rollouts/`.
- Whether the success envelope kind is `"Rollout"` vs `"RolloutCreate"`.
- Whether to expose `--extension-duration` (guarded-only API field).

## Deferred Ideas

- Preflight (`recommended-duration` + `--skip-health-checks` + TTY prompt + audit shape) → new phase within this project.
- Idempotency-Key → out of scope for entire project; strike from REQUIREMENTS.md.
- Metric groups (`isGroup: true`) → v1.1.
- `--ref` for existing-rule selection → future multi-instruction-patch phase if/when added.
- `--clauses` for new-rule targeting → future demand-driven.
- Generic CLI-robustness features (idempotency, numeric exit-code taxonomies, structured retry contracts) → tackled CLI-wide outside this project, not per-subtree.
