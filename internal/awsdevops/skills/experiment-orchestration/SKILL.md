---
name: "experiment-orchestration"
description: "Takes a product goal through a LaunchDarkly experiment and, if it wins, a guarded rollout (or a regular rollout on plans without guarded rollouts). Uses GitHub for the code change and deployment when it is connected to the agent space; otherwise hands those steps to a person."
license: Apache-2.0
compatibility: Requires the remotely hosted LaunchDarkly MCP server in an AWS DevOps Agent space. GitHub is optional.
metadata:
  author: launchdarkly
  version: "0.1.0"
---

# Automated Experimentation

Use this skill when you have a goal you want to move through experimentation (e.g., "increase checkout conversion by 15%", "decrease page load time by 20%").

**Core principle: experiment first, then a guarded rollout, or a regular rollout if the plan has no guarded rollouts.** Always prove a change on a small, fixed slice of traffic via an A/B experiment before rolling it out. Never roll out blind — the rollout exists only to scale a change the experiment has already shown to work.

**Execution mode:** once the goal is confirmed (Step 1), run Steps 2–8 end-to-end. Async operations (code implementation, release review, deployment, experiment monitoring, rollout monitoring) should be checked periodically, not tight-polled — see the waiting note in each step. Stop and ask the user only for a planned handoff (merging the PR; any code, review, or deploy step you hand over without GitHub) or if a step fails unrecoverably (repeated failed release reviews, deployment failure, or an inconclusive/losing experiment result).

**Approvals:** if the LaunchDarkly environment requires approval for a change (for example, to turn a flag on or off, start or stop an experiment, or start a guarded rollout), create an approval request instead of the direct change. The first time you change an entity (a flag, an experiment) in an environment, call the change tool directly; don't guess beforehand whether the environment requires approval. If the response has `requiresApproval: true`, tell the user the change was not made because it needs LaunchDarkly approval, ask who should review it, and submit the returned instructions with `create-approval-request` (or the approval tool that response names). From then on, send every change to that entity in that environment straight to an approval request with the same instructions you would give the direct call, instead of calling the change tool first. Tell the person who must review it. Continue only after `list-approval-requests` shows it approved, then apply it with `apply-approval-request` and go to the next step. If it still shows pending, stop and tell the user it is waiting for review; never apply a pending request, and don't keep re-checking in the same turn. If a reviewer rejects it, stop and report.

**Blocked tools:** an agent space can block MCP tools that it marks destructive. Likely candidates include, but aren't limited to, `toggle-flag`, `start-experiment-iteration`, `update-rollout`, `update-targeting-rules`, `start-guarded-rollout`, and `stop-experiment-iteration`. A blocked call fails without a LaunchDarkly error, or returns nothing, so you can't tell why it failed. If a write fails without a LaunchDarkly error that explains it, or returns nothing, first read the state back (`get-flag` or `get-experiment`): an empty response may still have made the change. If it didn't, don't retry: tell the user the tool may be blocked as destructive in this agent space, and hand them the step as below.

**When a step can't succeed through you:** a blocked tool, or any other write you can't complete (including `create-experiment`, `create-metric`, and the approval tools). Hand the step to the user with exact instructions, not a summary. Give the link, what to click, and every value the call would have sent, using the real names and keys from this run: variation names, percentages, metric keys, thresholds, randomization unit, stage weights and durations, the winning treatment. If the environment requires approval, tell them the UI will ask them to submit the change for review and who should review it. Then wait until they confirm it's done, read the state back to check it, and continue from the next step. For example:
- Turn the flag on or off, or change its default rule or off variation: open `https://app.launchdarkly.com/projects/<project-key>/flags/<flag-key>/targeting?env=<environment-key>&selected-env=<environment-key>`, set the toggle, the default rule, or the off variation to the exact variation name or percentages, and save.
- Start a guarded rollout: on the same targeting page, set the default rule to a guarded rollout and give the variation to roll out, the randomization unit, each metric key with its regression threshold and roll back on regression, and each stage's percentage and monitoring window from Step 7.
- Start or stop the experiment: open the `designUrl` that `create-experiment` returned, or `https://app.launchdarkly.com/projects/<project-key>/experiments/<experiment-key>/design?env=<environment-key>&selected-env=<environment-key>`. When the user stops it, tell them to choose Control as the winner, for the reason Step 7 gives.

**Plan limits:** if LaunchDarkly returns an error saying the plan doesn't include a feature this skill needs, and this skill has no fallback for it (it has one only for guarded rollouts), tell the user which feature is missing and that it needs a plan that includes it, then stop. Treat a failure as a plan limit only when the error says so; a silent failure is a possibly blocked tool, above. If the account is on another LaunchDarkly instance (for example, federal), use that instance's host instead of `app.launchdarkly.com`.

**GitHub:** Steps 2–5 use GitHub when it is connected to the agent space. Before Step 2, check whether you have GitHub access to the target repository. If you don't, read no code: ask the user where the change belongs, and in Step 3 give them the flag key and the change to make, then wait until they confirm the change is reviewed, merged, and deployed, and skip to Step 6. Triggering workflows and proposing fixes need the GitHub connection's Read and write permissions; with Read-only, hand those steps to the user.

**Always produce the Step 8 report on every terminal outcome.** The moment an experiment or rollout reaches a terminal outcome — winner, loser, inconclusive, or rollback — produce the full report in the same turn you announce the outcome. Don't let a casual "it worked!" substitute for the structured report.

## Step 1: Goal Clarification

Before doing anything, get answers to:

1. **What metric measures success?** *(Required)* e.g. conversion rate, page load time, bounce rate. Reserve your error-rate metric as a safety guardrail — never use it as the primary success metric.
2. **What's the target improvement?** *(Required)* e.g. 15% increase, 200ms decrease.
3. **What's the current baseline?** *(Optional — infer from production metrics if not given)*
4. **What parts of the app are in scope?** *(Optional — infer from the codebase if not given)*
5. **Any constraints?** *(Optional)* e.g. no changes to the payment flow.
6. **How much regression should roll a change back?** *(Optional — propose one for the success metric and each guardrail metric if not given)* Used as each metric's `regressionThreshold` in Step 7.

Questions 1–2 are required before proceeding: until the user has answered both, make no changes (no flag, metric, or experiment). If a question gets no answer, ask again or stop; never assume an answer. Infer 3–6 where possible and confirm your assumptions with the user before implementing.

**Find the metrics.** The experiment needs the success metric. LaunchDarkly requires at least one metric to start a guarded rollout, and this skill requires an error-rate metric as a guardrail in both the experiment and the rollout. Find them before Step 2:

1. Choose the randomization unit (typically "user"). Every metric must list it in its randomization units, or the guarded rollout rejects the metric.
2. Call `list-metrics` with a `query` for the success metric. For errors, look first for LaunchDarkly's built-in error-rate metric (key ending in `telemetry-default-error-rate`, event `$ld:telemetry:error`), then `query: "error"`. Check that each metric's randomization units include the unit from item 1.
3. For the success metric, call `list-metric-events` with `query` set to its event key, in the environment the experiment will use. It returns at most 50 keys, so always pass the query. Don't run this check for the error-rate metric: no recent error events may just mean no errors.
4. Confirm the metric keys with the user. If a metric doesn't exist, plan to create it in Step 3: Implementation. If the success metric exists but its event isn't arriving, plan to add its `track()` call in Step 3: Implementation instead of creating a second metric.

## Step 2: Hypothesis Generation

Explore the target repository/codebase to find a plausible change:

1. Search and read the relevant code paths.
2. Think through what UI/UX or logic change could plausibly move the chosen metric.
3. Check whether this hypothesis (or something close to it) has already been tried and failed — look at flag history or archived flags with similar naming. Avoid repeating a known failure.
4. Present the hypothesis to the user before proceeding, along with your reasoning and any inferred assumptions from Step 1. If the user wants to test more than one version of the change (for example, three button labels), list each version; Step 3 then creates a multivariate flag. Each version splits the same 10% of traffic, so more than three versions makes significance slower to reach; say so to the user.

**Before finalizing a flag key, check for collisions:** look up any candidate flag key first.
- Already fully shipped (100% one variation, no split) → already decided, pick a different hypothesis.
- Actively running an experiment → mid-flight, don't compete with it, pick a different hypothesis.
- Doesn't exist → safe to create.

## Step 3: Implementation

1. Create the feature flag, OFF by default in all environments, with `clientSideAvailability` of `{"usingEnvironmentId": true, "usingMobileKey": true}` so that every SDK can read it. Pick its kind from the hypothesis:
   - **One version of the change:** a boolean flag with `defaults` of `{"onVariation": 1, "offVariation": 1}`, so that it serves `false` (the control) even when it is on.
   - **Several versions of the change:** a multivariate flag with `kind: "multivariate"` and `variations` listing the control (current behavior) first, then one variation per version, each with a `name` and the `value` the code reads. Set `defaults` to `{"onVariation": 0, "offVariation": 0}`, so that it serves the control even when it is on.

   If `create-flag` errors on `defaults`, create the flag without `defaults`, then call `update-off-variation` with `allEnvs: true`, `variationIndex` and `fallthroughVariationIndex` both set to the control's index (`1` for a boolean flag, `0` for a multivariate flag), and `updateFlagDefaults: true`. Do this right after creating the flag, before any code that reads it is deployed: without it, the flag can serve a treatment to everyone, off or on. Environments that need approval come back blocked with instructions for `create-approval-request`; the others still apply.

   Then read the flag back with `get-flag` in the environment the experiment will use: its off variation and default rule must both be the control. If they aren't, hand the change to the user (see "When a step can't succeed through you") and don't continue until they are.

   Name it with a clear pattern like `exp-<metric>-<short-description>` (e.g. `exp-checkout-conversion-cta-color`), lowercase with hyphens, ~50 chars max. If the project has its own flag naming conventions (for example, a required prefix or case), follow them instead.
2. If Step 1: Goal Clarification found no success or error-rate metric, create it now with `create-metric`, with `randomizationUnits` set to the unit from Step 1: Goal Clarification. For an error-rate metric, use `kind: "custom"`, `measureType: "occurrence"` (the share of contexts that hit an error), and `successCriteria: "LowerThanBaseline"`. Include the `track()` call for each new or silent metric's event key in the code change below.
3. If a coding agent is available, hand off the implementation to it, with instructions to gate the change behind the exact flag key from item 1 above. Without a coding agent or GitHub write access, give the user the flag key and the change instead, and wait for their PR (with GitHub connected) or their confirmation that it is deployed (without GitHub).
4. This step is asynchronous — check status periodically rather than looping tightly on it.
5. Once implementation completes, move to Step 4: Release Readiness with the resulting branch/PR. If it fails, report the error and stop.

## Step 4: Release Readiness

Run an AWS DevOps Agent release readiness code review on the PR before it merges. It runs automatically on connected repositories with **Auto trigger change review** enabled; otherwise request one.

- If it passes: ask the user to merge the PR, and wait until they confirm it is merged.
- If it fails: feed the review's specific feedback back into implementation (or to the user, if they made the change) and retry. Cap retries at a small fixed number (e.g. 3 attempts total) — if it still hasn't passed, stop and report the last failure to the user rather than retrying indefinitely.

Never skip the review. Without GitHub, ask the user to have the PR reviewed and merged.

## Step 5: Deployment

The flagged code must be running in the environment the experiment uses before Step 6 turns the flag on; otherwise the experiment measures nothing. Confirm the merged change is deployed there. If the deploy workflow runs only when triggered (for example, `workflow_dispatch`), trigger it as below.

1. Trigger the deploy workflow on the merge target branch (this needs Read and write GitHub access; otherwise ask the user to deploy and confirm when it's done). Treat "already an in-progress deployment for this ref" as expected de-duplication, not an error — don't re-trigger.
2. Check the workflow run's status periodically rather than looping tightly.
3. If a run reports "running" for far longer than normal, check the workflow run history for that commit before assuming it's still in progress.
4. **Trigger a deployment at most once per attempt.** If you're unsure whether a previous trigger succeeded, check status first — never re-trigger just because you're unsure.
5. On timeout: stop, check the CI run directly, report the situation, ask how to proceed.
6. On explicit failure: stop and report — do not proceed to the experiment.
7. On success: proceed immediately to Step 6: Experiment Phase.

## Step 6: Experiment Phase (fixed 10%)

Prove the change on a small, fixed slice of traffic. The experiment and the rollout never run at the same time: a guarded rollout can't start while an experiment is running on the flag, so Step 7 stops the experiment iteration before Phase 2 starts any rollout.

The control is the variation that keeps current behavior: `false` on a boolean flag, the first variation on a multivariate flag. Every other variation is a treatment: `true` on a boolean flag, one per version on a multivariate flag.

1. Turn the flag ON; starting an experiment fails while the flag is off. It still serves the control to everyone, because Step 3: Implementation set its defaults to the control.
2. Read the flag to get its current version (required by `create-experiment`) and the ID of every variation.
3. Create an experiment with:
   - `flags`: `{"<flag-key>": {"ruleId": "fallthrough", "flagConfigVersion": <version from item 2 above>, "notInExperimentVariationId": "<control variation ID>"}}`. The experiment runs on the default rule, the only rule the Step 7 rollouts act on, and every context outside the experiment keeps getting the control.
   - `hypothesis`: the hypothesis from Step 2: Hypothesis Generation.
   - `treatments`: one per variation. The control is `{"name": "Control", "baseline": true, "allocationPercent": <share>, "parameters": [{"flagKey": "<flag-key>", "variationId": "<control variation ID>"}]}`; each treatment is the same with its variation's name, `"baseline": false`, and its variation ID. Unless the user chose another split, split 10% of traffic evenly: each `<share>` is 10 divided by the number of variations, rounded down to two decimals (2 variations: 5 each; 3: 3.33 each; 4: 2.5 each). Each `allocationPercent` is that treatment's share of all traffic on the rule, so the shares add up to 10%. Never use 50 and 50, or any shares that add up to more than 10: 50 and 50 puts 100% of traffic in the experiment.
   - `canReshuffleTraffic: false`, so contexts keep their variation if the traffic changes.
   - `randomizationUnit`: the randomization unit from Step 1: Goal Clarification.
   - `metrics`: the success metric from Step 1: Goal Clarification and the guardrail metrics, each as `{"key": "<metric-key>"}`. Always include your error-rate metric as a guardrail; add a performance metric (e.g. p95 page load time) too if this is a performance-focused change.
   - `primarySingleMetricKey`: the success metric's key. It is the only primary metric.
   - `methodology: "frequentist"` with `analysisConfig` `{"sequentialTestingEnabled": true, "significanceThreshold": "5", "testDirection": "two-sided"}` (the last two are LaunchDarkly's defaults, sent so the config is complete). With two or more treatments, also set `"multipleComparisonCorrectionMethod": "benjamini-hochberg"` and `"multipleComparisonCorrectionScope": "variations"`; LaunchDarkly rejects a method without a scope, and without the correction, comparing several treatments to the control declares more false winners. Step 7 checks results many times; sequential testing keeps significance valid under repeated checks, where a fixed-horizon test would declare false winners. Don't use the Bayesian methodology: it reports a probability of being best, not the significance Step 7 decides on.
4. Start the experiment iteration. Starting it writes the split onto the default rule: 10% of contexts in the experiment, split across the variations, and the other 90% on the control, not analyzed.
5. Move to Step 7 to monitor toward a decision.

## Step 7: Monitoring & Outcome

Check status periodically — don't tight-loop. In an interactive session, check once and report progress, then pick back up later. Every MCP write needs a person to approve it, so an unattended run stops at its first write: run checks that may lead to a write in a session where someone can approve, and report progress so the next session can pick it up.

**Phase 1 — Prove the experiment at 10% (gate before any rollout):**

Watch for statistical significance on the primary metric. For each metric, call `get-experiment-metric-results`: it returns per treatment `analyzedUnitCount` (contexts in the experiment), `conversionCount` (contexts that sent the metric's event), and `isSignificant`. Decide on `isSignificant`, not on the `isBest` that `get-experiment-results` reports; `isBest` marks the leading treatment even when nothing is significant.

**Check the instrumentation after the first day.** If the success metric, or any guardrail other than the error-rate metric, has a `conversionCount` of 0 in every treatment, stop and report an instrumentation problem instead of waiting for significance. Zero error events is fine: it means no errors.

Judge each treatment against the control on its own. A treatment **qualifies** if it has a significant positive lift on the primary metric and no guardrail regression. A treatment **fails** if it has a significant negative lift on the primary metric or a guardrail regression.

- **Proven:** at least one treatment qualifies, even if another fails. The winner is the qualifying treatment with the largest lift. Stop the experiment iteration with control as the winner, then move to Phase 2. Stopping serves the winner to the experiment's targeting rule (everyone, when the experiment is on the default rule), so a treatment winner would skip the guarded rollout. If the plan has no guarded rollouts, still stop with control as the winner and use the regular rollout in Phase 2.
- **Loser:** no treatment qualifies, and either every treatment fails or any treatment has a guardrail regression. A regressing treatment must not keep serving traffic while the others collect data. Declare a loser, stop the iteration with control as the winner, turn the flag off (do not archive it while the code still evaluates it), skip Phase 2, go straight to the Step 8 report.
- **Anything else** (no significance, or mixed results such as one treatment worse and another flat): keep collecting data. At a reasonable ceiling (e.g. 4 weeks), report "inconclusive, need more traffic", naming any treatment that was significantly worse, stop the iteration with control as the winner, and stop; don't proceed to Phase 2.

To stop the iteration, pass `stop-experiment-iteration` the `_id` of the control treatment (`baseline: true`) from `get-experiment` as `winningTreatmentId`, in every outcome; it takes a treatment ID, not a variation ID. Stopping with the real winner would serve it to everyone and skip the rollout.

Never declare a winner off a single data point or before your stats engine confirms significance. Also require an `analyzedUnitCount` of at least 100 in each variation before acting on a significant result; with fewer, keep collecting data.

**Phase 2 — Guarded rollout ramp (only after Phase 1 proves the change):**

Start a guarded rollout with:
- `testVariationId`: the winning treatment's variation ID; `controlVariationId`: the control variation ID (both from Step 6).
- Same randomization unit as the experiment.
- **Exactly 3 monitored stages, each below 50%** — e.g. 20% → 30% → 40%, ~60 minutes monitoring each. `rolloutWeight` is in thousandths of a percent (20% is `20000`) and `monitoringWindowMilliseconds` is in milliseconds (60 minutes is `3600000`). Don't add a 100% stage.
- `updateDefaultRuleOnCompletion: true`, so the rollout moves the default rule to the winning variation once the final stage completes cleanly.
- Every metric the experiment used, primary and guardrails, each with `onRegression` set to notify and roll back, and a `regressionThreshold` (required) from Step 1.

Track stage progression. If the rollout rolls back or stops at any point, treat it as a regression: declare failed, turn the flag off (do not archive it while the code still evaluates it), and go to the Step 8 report.

Once the final stage completes cleanly and the default rule serves the winning variation, declare a winner and go to the Step 8 report.

**If the plan has no guarded rollouts:** if `start-guarded-rollout` returns an error saying guarded rollouts aren't available on the plan, use a regular rollout instead. A silent failure is not a plan error: read the flag back and, if no rollout started, hand the guarded rollout to the user (see "When a step can't succeed through you") instead of rolling out to everyone.
1. Read the flag's variations to find the indexes of the winning treatment's variation and the control's. Call `update-rollout` with `rolloutType: "variation"` and the winner's index on the default rule. This serves the winner to everyone at once, with no automatic rollback.
2. Check the guardrail metrics once per monitoring window, for about as long as the guarded rollout would have run (e.g. 3 hours). Reverting is a write that needs a person's approval, so run this watch in a session where someone can approve.
3. If a guardrail regresses, call `update-rollout` with the control's index, declare failed, and go to the Step 8 report. Otherwise, declare a winner and go to the Step 8 report.

**Retrying after a guarded rollout rollback:** a rollback isn't always caused by your monitored metrics genuinely regressing — it can also be triggered by an unrelated application error surfacing mid-ramp. Before blindly restarting after the user says they've fixed something:
1. Confirm the flag's current state (should be back to 100% control, nothing stuck mid-rollout).
2. Check the change history timing between "advanced to next stage" and "reverted." A rollback within seconds of advancing is inconsistent with a full metric-window regression and points to an external cause instead.
3. If the flag is cleanly reverted and the external cause is confirmed fixed, it's safe to restart the guarded rollout from scratch with the same parameters.
4. Don't silently retry without this check, and don't refuse to retry just because a prior attempt rolled back — a genuinely fixed external cause is a legitimate reason to retry. A metric-driven loser is not — don't retry that.

**On any terminal outcome, immediately produce the Step 8 report in the same turn** — a one-line "it worked!" note is fine as a lead-in, but the structured report must follow, not wait for a follow-up request.

## Step 8: Report

Runs automatically the instant Step 7 reaches a terminal outcome (winner, with the default rule serving it; loser; inconclusive; or rollback/failure). Use this exact structure from Experiment Report to Next Steps:

```markdown
## Experiment Report: [Goal Description]

**Date:** [YYYY-MM-DD]
**Goal:** [metric] [direction] by [target]%
**Status:** [achieved / in progress / stalled]

### Hypothesis
[What we tried and why]

### Implementation
- Flag: [flag_key] ([boolean / multivariate])
- Variations: [control, then each treatment]
- Files modified: [list]
- Branch: [branch name]

### Release Readiness
- [release readiness review passed after N attempt(s) / reviewed by the user]

### Experiment Phase (10% fixed split)
- Status: [proven / loser / inconclusive]
- Winner: [variation name, or none]
- Duration: [time]
- Metric change: [before] → [after] ([+/-]%)
- Statistical significance: [value, confidence interval]

### Rollout Phase (if reached)
- Type: [guarded rollout / regular rollout (no guarded rollouts on this plan)]
- Status: [completed / rolled_back / not started]
- Duration: [time]
- Stages reached: [N of 3 monitored stages, guarded rollout only]
- If rolled back and retried: [root cause, outcome of retry]

### Safety Metrics
- [error-rate metric key]: [baseline] → [final] ([no regression / regression detected])
- [other guardrails]: [baseline] → [final] ([status])

### Next Steps
[What to do next based on the outcome]
```

## Safety Rules

- Always present the hypothesis before implementing.
- Always have the PR reviewed before merging: the release readiness review with GitHub, or the user's review without it. Never skip it.
- **Always prove a change via a fixed small-percentage experiment before starting any rollout** — never roll out blind.
- Always include an error-rate (or equivalent "don't break prod") metric as a guardrail, separate from your success metric.
- Add a performance guardrail (e.g. p95 latency) for performance-focused changes.
- Every guarded rollout metric should be configured to both notify AND auto-rollback on regression — don't rely on notification alone. A regular rollout has no automatic rollback, so watch its guardrail metrics after rolling out.
- **Keep guarded rollout stages below 50%** and let the rollout move to 100% after the final stage — don't add an explicit 100% stage.
- Distinguish a metric-driven rollback (don't retry) from an external-cause rollback (safe to retry once fixed) before restarting a rolled-back rollout.
- Produce the Step 8 report on every terminal outcome — never defer it to a follow-up ask.
