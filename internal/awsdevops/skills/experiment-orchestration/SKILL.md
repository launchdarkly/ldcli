---
name: "experiment-orchestration"
description: "Orchestrates automated experimentation lifecycle using LaunchDarkly Guarded Rollouts, an AI coding agent for implementation, and GitHub Actions for deployment."
---

# Automated Experimentation

Use this skill when you have a goal you want to move through experimentation (e.g., "increase checkout conversion by 15%", "decrease page load time by 20%").

**Core principle: experiment first, then guarded rollout.** Always prove a change on a small, fixed slice of traffic via an A/B experiment before ramping it up through a guarded rollout. Never start a guarded rollout blind — it exists only to scale a change the experiment has already shown to work.

**Execution mode:** once the goal is confirmed (Step 1), run Steps 2–8 end-to-end, one step per invocation while you are resuming. Async operations (code implementation, release review, deployment, experiment monitoring, rollout monitoring) should be checked periodically, not tight-polled — see the waiting note in each step. Only stop and ask the user something if a step fails unrecoverably (repeated failed release reviews, deployment failure, or an inconclusive/losing experiment result), or if a person must implement and deploy the change (Step 3).

**Resuming:** an experiment runs for weeks, so most invocations pick up work that an earlier one started. Always follow "Resuming in-flight work" below first. Start at Step 1 only for a goal that no open record already covers.

**The final report (Step 8) is mandatory, not optional.** The moment an experiment or rollout reaches a terminal outcome — winner, loser, inconclusive, or rollback — produce the full report in the same turn you announce the outcome. Don't let a casual "it worked!" substitute for the structured report.

## Experiment Record

Keep one record per flag in the durable store your environment provides, and update it whenever the step changes. A later invocation has no memory of this one, so the record must hold everything it needs:

- Flag key and project and environment keys
- Goal: metric, direction, target, and baseline
- Hypothesis
- Current step (for example, "Step 3: waiting for deployment", "Step 7: Phase 1 running", "Step 8: report produced")
- Implementation details for the report: files, branch, and release readiness result ("implemented by a person" when no coding agent did it)
- Deployment confirmation: who confirmed that the change is deployed, and when
- Experiment key, start date, and stop date
- Guardrail metric baselines at the experiment start
- Guarded rollout start date and end date

Every flag this skill creates carries the tag `experiment-orchestration`, and its description holds the goal and the hypothesis. This marks the flags this skill owns, and it keeps the goal recoverable if the store is lost.

## Resuming in-flight work

1. Read every experiment record that is not at "Step 8: report produced". If you have no store, list the flags that are tagged `experiment-orchestration` and are not archived, and rebuild the records from their descriptions and their experiments. Never act on a flag without this tag.
2. For each one, read the current state from LaunchDarkly (flag, experiment, guarded rollout) and compare it with the record.
3. Advance each one by at most one step, following the step in this skill that the record names. A terminal outcome also produces its Step 8 report in the same invocation. Never start a new goal or accept a new hypothesis while resuming; those need a person.
4. Update the record. If nothing is in flight, say so and stop.

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

1. Create a boolean feature flag, OFF by default in all environments, with the tag `experiment-orchestration` and a description that holds the goal and the hypothesis. Name it with a clear pattern like `exp-<metric>-<short-description>` (e.g. `exp-checkout-conversion-cta-color`), lowercase with hyphens, ~50 chars max. If the change is in a browser or mobile app, make the flag available to client-side SDKs (`clientSideAvailability.usingEnvironmentId`, or `usingMobileKey` for mobile); otherwise the app cannot read the flag and the experiment collects no data.
2. Create the experiment record (see "Experiment Record").
3. Hand off implementation to your coding agent/tool of choice, with clear instructions to gate the change behind the exact flag key from step 1.
4. This step is asynchronous — check status periodically rather than looping tightly on it.
5. Once implementation completes, move to Step 4 with the resulting branch/PR. If it fails, report the error and stop.

**If no coding agent is available:** report the flag key and the change to make, set the record to "Step 3: waiting for deployment", and stop. A person implements, reviews, and deploys the change. Continue at Step 6 (skipping Steps 4–5) only when one of these is true:

- A person says in this invocation that the change is deployed. Write their confirmation to the record.
- The record already holds a person's deployment confirmation.

Until then, a resumed invocation reports "waiting for deployment" for this flag and changes nothing. Flag evaluations from the app show that the code evaluates the flag, but not that the treatment works, so they never replace a person's confirmation.

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
2. Create an experiment on the flag's default rule (`"fallthrough"`) with:
   - Audience allocation of 10% of the rule's traffic (a flat allocation, not a staged ramp) on your chosen randomization unit (typically "user"). The other 90% receive the control variation and are not analyzed.
   - Treatments: control (off) at 50%, treatment (on) at 50%. Treatment allocations must sum to 100%; the 10% is the audience allocation, not the treatments.
   - Exactly one primary metric: the success metric from Step 1. If it does not exist, create the metric and the event that feeds it first.
   - Guardrail metric(s): always include your error-rate metric; add a performance metric (e.g. p95 page load time) too if this is a performance-focused change.
   - Sequential testing, if your methodology offers it, so that checking results on every invocation does not inflate false positives.
3. Start the experiment/data collection.
4. Record the experiment key, the start date, and the guardrail baselines, and set the record to "Step 7: Phase 1 running".
5. Move to Step 7 to monitor toward a decision.

## Step 7: Monitoring & Outcome

Check status periodically — don't tight-loop. In an interactive session, check once and report progress, then pick back up later. In an unattended/scheduled context, check once per invocation and persist your progress somewhere durable between runs.

**Phase 1 — Prove the experiment at 10% (gate before any rollout):**

Run the experiment for at least 2 full weeks, so that weekday and weekend behavior are both in the data. Check results once per invocation (daily is enough), and decide on the primary metric:

- **Guardrail regression at any time** → stop early with control as the winner (see "Ending without a win" below).
- **Before 2 weeks** → keep running, even if the primary metric looks significant. Report progress only.
- **After 2 weeks, significant + positive lift** → experiment proven. Stop the iteration with the treatment as the winner and move to Phase 2.
- **After 2 weeks, significant + negative lift** → declare a loser and end without a win.
- **No significance after 4 weeks** → declare "inconclusive, need more traffic" and end without a win.

**Ending without a win:** stop the iteration with control as the winner and a reason that says why, because stopping an iteration requires a winning treatment. Stopping does not change targeting, so then turn the flag off so that everyone gets control. Record the stop date, skip Phase 2, and go to the Step 8 report. Don't archive the flag: the code still evaluates it. Recommend removing the flag from the code, and then archiving it, in the report's Next Steps.
 Never declare a winner off a single data point or before your stats engine confirms significance.

**Phase 2 — Guarded rollout ramp (only after Phase 1 proves the change):**

Start a guarded rollout with:
- The winning ("on") variation as the test, the original as control.
- Same randomization unit as the experiment.
- **Exactly 3 monitored stages, none above 50%** — e.g. 10% → 25% → 50%, ~24 hours monitoring each. LaunchDarkly rejects any step above 50%, because each step needs an equal split between the original and new variations, and the rollout auto-promotes to 100% itself once the final monitored stage completes cleanly — no explicit 100% stage needed.
- The same primary + guardrail metrics as the experiment, each configured to notify and auto-rollback on regression.

LaunchDarkly advances the stages and rolls back on its own, so each invocation only reads the rollout status and updates the record. If the rollout rolls back or stops at any point, treat it as a regression: declare failed, confirm that everyone gets control, and go to the Step 8 report. Recommend flag cleanup in Next Steps rather than archiving the flag yourself.

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
- **Run an experiment at least 2 full weeks** before deciding, unless a guardrail regresses.
- **Keep every guarded rollout stage at or below 50%** (LaunchDarkly rejects larger steps) and let the platform auto-promote to 100% after the final stage — don't try to add an explicit 100% stage.
- Update the experiment record on every step change, so that the next invocation can resume.
- Distinguish a metric-driven rollback (don't retry) from an external-cause rollback (safe to retry once fixed) before restarting a rolled-back rollout.
- The final report is automatic and mandatory on every terminal outcome — never defer it to a follow-up ask.
