---
name: "experiment-orchestration"
description: "Orchestrates automated experimentation lifecycle using LaunchDarkly Guarded Rollouts, an AI coding agent for implementation, and GitHub Actions for deployment."
---

# Automated Experimentation

Use this skill when you have a goal you want to move through experimentation (e.g., "increase checkout conversion by 15%", "decrease page load time by 20%").

**Core principle: experiment first, then guarded rollout.** Always prove a change on a small, fixed slice of traffic via an A/B experiment before ramping it up through a guarded rollout. Never start a guarded rollout blind — it exists only to scale a change the experiment has already shown to work.

**Execution mode:** once the goal is confirmed (Step 1), run Steps 2–8 end-to-end. Async operations (code implementation, release review, deployment, experiment monitoring, rollout monitoring) should be checked periodically, not tight-polled — see the waiting note in each step. Only stop and ask the user something if a step fails unrecoverably (repeated failed release reviews, deployment failure, or an inconclusive/losing experiment result).

**The final report (Step 8) is mandatory, not optional.** The moment an experiment or rollout reaches a terminal outcome — winner, loser, inconclusive, or rollback — produce the full report in the same turn you announce the outcome. Don't let a casual "it worked!" substitute for the structured report.

## Step 1: Goal Clarification

Before doing anything, get answers to:

1. **What metric measures success?** *(Required)* e.g. conversion rate, page load time, bounce rate. Reserve your error-rate metric as a safety guardrail — never use it as the primary success metric.
2. **What's the target improvement?** *(Required)* e.g. 15% increase, 200ms decrease.
3. **What's the current baseline?** *(Optional — infer from production metrics if not given)*
4. **What parts of the app are in scope?** *(Optional — infer from the codebase if not given)*
5. **Any constraints?** *(Optional)* e.g. no changes to the payment flow.

Questions 1–2 are required before proceeding; infer 3–5 where possible and confirm your assumptions with the user before implementing.

## Step 2: Hypothesis Generation

Explore the target repository/codebase to find a plausible change:

1. Search and read the relevant code paths.
2. Think through what UI/UX or logic change could plausibly move the chosen metric.
3. Check whether this hypothesis (or something close to it) has already been tried and failed — look at flag history or archived flags with similar naming. Avoid repeating a known failure.
4. Present the hypothesis to the user before proceeding, along with your reasoning and any inferred assumptions from Step 1.

**Before finalizing a flag key, check for collisions:** look up any candidate flag key first.
- Already fully shipped (100% one variation, no split) → already decided, pick a different hypothesis.
- Actively running an experiment → mid-flight, don't compete with it, pick a different hypothesis.
- Doesn't exist → safe to create.

## Step 3: Implementation

1. Create a boolean feature flag, OFF by default in all environments. Name it with a clear pattern like `exp-<metric>-<short-description>` (e.g. `exp-checkout-conversion-cta-color`), lowercase with hyphens, ~50 chars max.
2. Hand off implementation to your coding agent/tool of choice, with clear instructions to gate the change behind the exact flag key from step 1.
3. This step is asynchronous — check status periodically rather than looping tightly on it.
4. Once implementation completes, move to Step 4 with the resulting branch/PR. If it fails, report the error and stop.

## Step 4: Release Readiness

Run your standard release/risk review on the PR before merging.

- If it passes: merge the PR.
- If it fails: feed the review's specific feedback back into implementation and retry. Cap retries at a small fixed number (e.g. 3 attempts total) — if it still hasn't passed, stop and report the last failure to the user rather than retrying indefinitely.

*(If your environment genuinely has no review capability available — e.g., a fully unattended automation context — you can skip straight to merge, but treat that as a deliberate, narrow exception you call out explicitly, not a default. Skipping review removes your only gate against shipping broken code.)*

## Step 5: Deployment

Deployment typically won't fire automatically on merge if your workflow is manually-triggered (`workflow_dispatch`-only) — you'll need to trigger it explicitly.

1. Trigger the deploy workflow on the merge target branch. Treat "already an in-progress deployment for this ref" as expected de-duplication, not an error — don't re-trigger.
2. Poll for status, but let your polling tool's own internal long-poll do the waiting rather than looping tightly yourself.
3. Watch for a "stale" status specifically: if a deployment reports "running" for far longer than normal, cross-check the actual CI run history by commit SHA/timing before assuming it's still in progress — a background poll process may have died without updating the record.
4. **Trigger a deployment at most once per attempt.** If you're unsure whether a previous trigger succeeded, check status first — never re-trigger just because you're unsure.
5. On timeout: stop, check the CI run directly, report the situation, ask how to proceed.
6. On explicit failure: stop and report — do not proceed to the experiment.
7. On success: proceed immediately to Step 6.

## Step 6: Experiment Phase (fixed 10%)

Prove the change on a small, fixed slice of traffic. Do **not** start a guarded rollout here — that's Step 7, and only after this proves out.

1. Turn the flag ON.
2. Configure a fixed 50/50 split across 10% of traffic (a flat allocation, not a staged ramp) on your chosen randomization unit (typically "user"). The remaining 90% of traffic is excluded from the experiment entirely.
3. Create an experiment with:
   - Exactly one primary metric: the success metric from Step 1.
   - Guardrail metric(s): always include your error-rate metric; add a performance metric (e.g. p95 page load time) too if this is a performance-focused change.
   - Treatments: control (off) at 50%, treatment (on) at 50%, allocated to 10% of total traffic.
4. Start the experiment/data collection.
5. Move to Step 7 to monitor toward a decision.

## Step 7: Monitoring & Outcome

Check status periodically — don't tight-loop. In an interactive session, check once and report progress, then pick back up later. In an unattended/scheduled context, check once per invocation and persist your progress somewhere durable between runs.

**Phase 1 — Prove the experiment at 10% (gate before any rollout):**

Watch for statistical significance on the primary metric:

- **Significant + positive lift** → experiment proven. Stop the experiment iteration and move to Phase 2.
- **Significant + negative lift** → declare a loser, archive the flag, skip Phase 2, go straight to the Step 8 report.
- **No significance after a reasonable ceiling (e.g. 30 minutes)** → report "inconclusive, need more traffic" and stop; don't proceed to Phase 2.

Never declare a winner off a single data point or before your stats engine confirms significance.

**Phase 2 — Guarded rollout ramp (only after Phase 1 proves the change):**

Start a guarded rollout with:
- The winning ("on") variation as the test, the original as control.
- Same randomization unit as the experiment.
- **Exactly 3 monitored stages, capped well below 100%** — e.g. 20% → 30% → 40%, ~60 minutes monitoring each. Don't add a stage at or above 100%; Guarded-rollout implementations reject stages above 50% audience allocation, and the rollout auto-promotes to 100% itself once the final monitored stage completes cleanly — no explicit 100% stage needed.
- The same primary + guardrail metrics as the experiment, each configured to notify and auto-rollback on regression.

Track stage progression. If the rollout rolls back or stops at any point, treat it as a regression: declare failed, clean up the flag (deprecate/archive it), and go to the Step 8 report.

Once the final stage completes cleanly and auto-promotes to 100%, declare a winner and go to the Step 8 report.

**Retrying after a rollback:** a rollback isn't always caused by your monitored metrics genuinely regressing — it can also be triggered by an unrelated application error surfacing mid-ramp. Before blindly restarting after the user says they've fixed something:
1. Confirm the flag's current state (should be back to 100% control, nothing stuck mid-rollout).
2. Check the change history timing between "advanced to next stage" and "reverted." A rollback within seconds of advancing is inconsistent with a full metric-window regression and points to an external cause instead.
3. If the flag is cleanly reverted and the external cause is confirmed fixed, it's safe to restart the guarded rollout from scratch with the same parameters.
4. Don't silently retry without this check, and don't refuse to retry just because a prior attempt rolled back — a genuinely fixed external cause is a legitimate reason to retry. A metric-driven loser is not — don't retry that.

**On any terminal outcome, immediately produce the Step 8 report in the same turn** — a one-line "it worked!" note is fine as a lead-in, but the structured report must follow, not wait for a follow-up request.

## Step 8: Report

Runs automatically the instant Step 7 reaches a terminal outcome (winner + auto-promoted to 100%; loser; inconclusive; or rollback/failure). Use this exact structure from Experiment Report to Next Steps:

## Experiment Report: [Goal Description]

**Date:** [YYYY-MM-DD]
**Goal:** [metric] [direction] by [target]%
**Status:** [achieved / in progress / stalled]

### Hypothesis
[What we tried and why]

### Implementation
- Flag: [flag_key]
- Files modified: [list]
- Branch: [branch name]

### Release Readiness
- [reviewed, passed after N attempt(s) / skipped, per your environment's process]

### Experiment Phase (10% fixed split)
- Status: [proven / loser / inconclusive]
- Duration: [time]
- Metric change: [before] → [after] ([+/-]%)
- Statistical significance: [value, confidence interval]

### Guarded Rollout Phase (if reached)
- Status: [completed / rolled_back / not started]
- Duration: [time]
- Stages reached: [N of 3 monitored stages]
- If rolled back and retried: [root cause, outcome of retry]

### Safety Metrics
- error-rate: [baseline] → [final] ([no regression / regression detected])
- [other guardrails]: [baseline] → [final] ([status])

### Next Steps
[What to do next based on the outcome]

## Safety Rules (the non-negotiables)

- Always present the hypothesis before implementing.
- Always run a release/risk review before merging, unless your environment has a deliberate, explicitly-called-out exception.
- **Always prove a change via a fixed small-percentage experiment before starting any guarded rollout** — never ramp blind.
- Always include an error-rate (or equivalent "don't break prod") metric as a guardrail, separate from your success metric.
- Add a performance guardrail (e.g. p95 latency) for performance-focused changes.
- Every rollout metric should be configured to both notify AND auto-rollback on regression — don't rely on notification alone.
- **Cap guarded rollout stages well below 100%** (most platforms reject stages ≥50% audience allocation) and let the platform auto-promote to 100% after the final stage — don't try to add an explicit 100% stage.
- Distinguish a metric-driven rollback (don't retry) from an external-cause rollback (safe to retry once fixed) before restarting a rolled-back rollout.
- The final report is automatic and mandatory on every terminal outcome — never defer it to a follow-up ask.
