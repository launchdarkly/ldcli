---
name: "experiment-orchestration"
description: "Start a LaunchDarkly experiment for a product goal, or check on running ones. Use when someone wants to move a goal (for example, increase checkout conversion by 15%) through a flag, an A/B experiment, and then a guarded rollout, or asks how their experiments are doing. Not for a standalone guarded rollout or flag change."
---

# Experimentation

Use this skill in chat when a person wants to move a goal through experimentation (for example, "increase checkout conversion by 15%"), or asks how their experiments are doing.

**Core principle: experiment first, then guarded rollout.** Always prove a change on a small, fixed slice of traffic with an A/B experiment before you ramp it up with a guarded rollout. A guarded rollout only scales a change that the experiment has already shown to work.

An experiment runs for weeks, so the work happens in separate chats:

1. **Start an experiment** (Steps 1–4): goal, hypothesis, flag, a person ships the change, then the experiment starts.
2. **Check on experiments** (Steps 5–6): read where each experiment stands, decide when a decision is due, and report.

Decide which one the person asked for. If they ask about experiments that already exist, go to "Check on experiments". Never start a goal that the person did not ask for.

## How you work

- **A person implements and deploys the code.** You cannot read or change the code, and you cannot deploy. You create and change things in LaunchDarkly only.
- **Every write needs the person's approval.** Before each write (create a flag, change a flag, start or stop an experiment, start a guarded rollout), say in one sentence what it changes. If the person rejects it, stop that step and do not try another way.
- **A blocked write is a setup problem.** If a write fails with "This action is not available in this agent space", the agent space does not allow LaunchDarkly writes: directed actions are off, or the tool is not allowed. Tell the person, and do not retry.
- **Change only your flags.** Change only flags that have the tag `experiment-orchestration`. Read other flags for collision checks and history, but never change them.
- **Stop at each wait.** When the next step needs a person (shipping code, or two weeks of data), say what happens next and end. Do not wait or poll in the chat.

## Experiment record

The next chat may be with another person, and you have no memory of this one. Keep the record in the description of the flag, and update it whenever a field changes:

- Project and environment keys
- Goal: metric, direction, target, and baseline
- Hypothesis, and the change that the person implements
- Current step, for example "waiting for deployment", "design waiting for confirmation", "experiment running", "won, guarded rollout pending", "guarded rollout running", or "report produced"
- Deployment: who confirmed that the change is deployed, and when

Read everything else (the experiment key, dates, results, and rollout stages) from LaunchDarkly.

# Start an experiment

## Step 1: Goal

Get answers to:

1. **What metric measures success?** *(Required)* For example, conversion rate, page load time, or bounce rate. Keep the error-rate metric as a safety guardrail; never use it as the primary metric.
2. **What is the target improvement?** *(Required)* For example, a 15% increase or a 200ms decrease.
3. **What is the baseline?** *(Optional: read it from the metric if the person does not know.)*
4. **What part of the product is in scope?** *(Required, because you cannot read the code.)*
5. **Which LaunchDarkly project and environment?** *(Required)* The environment is the one that the app's production traffic evaluates in. Confirm both; do not guess.
6. **Any constraints?** *(Optional)* For example, no changes to the payment flow.

## Step 2: Hypothesis

You cannot read the code, so build the hypothesis with the person:

1. Ask what the product surface does today and what change could move the metric.
2. Use LaunchDarkly for context: existing flags and metrics, archived flags with similar keys, and the change history. Avoid a hypothesis that was already tried and failed.
3. Propose one hypothesis in this shape: "Changing **X** will improve **metric** for **audience**, because **reason**." Confirm it with the person before you continue.

**Before you pick a flag key, check for collisions:**

- The key exists: pick a different key. If that flag tests the same idea, check its result first.
  - Already shipped (100% one variation, no split): the idea is already decided, so pick a different hypothesis.
  - An experiment is running on it: do not compete with it, so pick a different hypothesis.
- Does not exist: safe to create.

## Step 3: Flag and handoff

1. Create a boolean flag, OFF in all environments, with the tag `experiment-orchestration` and `defaults` of `{"onVariation": 1, "offVariation": 1}`, so that it serves false whether it is on or off. A flag that is on and matches no rule serves its on variation, so without these defaults, turning it on would give every user the treatment before the experiment starts. Name it `exp-<metric>-<short-description>` (for example `exp-checkout-conversion-cta-color`), lowercase with hyphens, about 50 characters at most. If the change is in a browser or mobile app, make the flag available to client-side SDKs (`clientSideAvailability.usingEnvironmentId`, or `usingMobileKey` for mobile). Otherwise the app cannot read the flag, and the experiment collects no data.
2. Write the experiment record to the flag description, with the step "waiting for deployment".
3. Give the person the handoff:
   - The flag key, and that the change must be behind it
   - The change, in plain words
   - What the code must do when the flag is off or cannot be evaluated: keep the current behavior
   - The event that the primary metric needs, if the app does not send it yet
4. Stop. The person implements, reviews, and deploys the change, then comes back.

## Step 4: Start the experiment

Start only after a person says that the change is deployed.

**Design the experiment:**

1. Make sure that the metrics exist. You can create a metric only for an event that the app already sends. If the event is not sent yet, ask the person to add it and come back.
2. Create the experiment as a draft, on the flag's default rule (`"fallthrough"`), in the environment from the record:
   - Only 10% of the rule's traffic takes part: treatments of control (false) at 5% and treatment (true) at 5%, with `notInExperimentVariationId` set to the false variation, so that the other 90% get control and are not analyzed. If the tool rejects allocations that do not sum to 100%, do not fall back to 50/50 of all traffic: tell the person, and ask them to set a 10% audience on the design page before they confirm.
   - Your randomization unit (usually "user").
   - Exactly one primary metric: the success metric from Step 1.
   - Guardrail metrics: always the error-rate metric, and a performance metric (for example p95 page load time) for a performance change.
   - `analysisConfig.sequentialTestingEnabled` set to true, so that checking results at each check-in does not inflate false positives.
3. Write the record: the step "design waiting for confirmation", the experiment key, and who confirmed the deployment, and when. A later chat then resumes the draft instead of making another one.
4. Show the person the design and its `designUrl`, check with them that 10% of traffic takes part, and wait for their confirmation. Do not start the experiment in the same reply.

**After the person confirms the design:**

5. Turn the flag on. Its defaults serve false, so no user gets the treatment yet. Starting an experiment needs the flag on.
6. Start the experiment.
7. Set the record to "experiment running". Tell the person to check back in 2 weeks, or sooner for a guardrail check, and end.

# Check on experiments

## Step 5: Read and decide

For each project in the person's account (or the one they name), list the flags with the tag `experiment-orchestration` that are not archived. For each one, read the record and the current state in the environment from the record, then report where it stands. Act only where a decision is due.

If the person asks about an experiment on a flag without the tag, read it and report on it, but never change it.

**Waiting for deployment:** report it. If the person says now that the change is deployed, go to Step 4.

**Design waiting for confirmation:** the draft experiment from the record exists but has not started. Show its `designUrl` again and ask the person to confirm it, then continue at item 5 of Step 4. Do not create another draft.

**Experiment running:**

- **A guardrail regressed, at any time:** end without a win (below).
- **Less than 2 full weeks of data:** keep it running, even if the primary metric looks significant. Report progress only.
- **2 weeks or more, significant positive lift:** the experiment is proven. First set the record to "won, guarded rollout pending", so that a later check-in can tell the win apart from a loss. Then stop the iteration with control as the winner, and a reason that says that the treatment won, with its lift. Stopping serves the winner to everyone that the default rule matches, so a treatment winner would reach 100% of users at once and skip the guarded rollout. Then start the guarded rollout (below), which ramps the treatment.
- **2 weeks or more, significant negative lift:** a loser. End without a win.
- **2 to 4 weeks, not significant:** keep it running. Report progress only.
- **No significance after 4 weeks:** inconclusive, need more traffic. End without a win.

Never declare a winner off a single data point, or before the stats engine confirms significance.

Sequential testing keeps each check-in valid, which is what lets a guardrail regression end the experiment early. The 2-week minimum for a win is about the product, not the statistics: behavior differs between weekdays and weekends, and a new experience can get a short novelty bump. A win needs at least two full weekly cycles.

**Ending without a win:** stop the iteration with control as the winner and a reason that says why, because stopping an iteration requires a winning treatment. Stopping serves control to everyone that the default rule matches. Then turn the flag off, so that its state shows that it is not in use. Do not archive the flag, because the code still evaluates it. Go to Step 6.

**Starting the guarded rollout:**

Before you start it, read the flag's default rule. It must serve control (false) to everyone. If it still has the experiment's split, set it to serve control first.

- The winning ("on") variation as the test, the original as control, and the same randomization unit as the experiment.
- Exactly 3 monitored stages, none above 50% (for example 10% → 25% → 50%), with about 24 hours of monitoring each. LaunchDarkly rejects any step above 50%, and it promotes the rollout to 100% itself after the last stage, so do not add a 100% stage.
- The same primary and guardrail metrics as the experiment, each set to notify and roll back on regression.

Set the record to "guarded rollout running". LaunchDarkly advances the stages and rolls back on its own, so later check-ins only read its status.

**Won, guarded rollout pending:** the iteration is stopped and the treatment won. Start the guarded rollout (above).

**Guarded rollout running:**

- **Still in a stage:** report the stage.
- **Rolled back or stopped:** the rollout failed. Confirm that everyone gets control, and go to Step 6.
- **Reached 100%:** a winner. Go to Step 6.

**Retrying after a rollback:** a rollback can come from an unrelated application error during the ramp, not from the change. Retry only when the person asks, and only after these checks:

1. The flag is back to 100% control, with nothing stuck mid-rollout.
2. The change history shows the timing between "advanced to next stage" and "reverted". A rollback within seconds of advancing does not fit a regression over a full metric window, so it points to an external cause.
3. The person confirms that the external cause is fixed.

Then restart the guarded rollout from the start with the same settings. Never retry a rollback that the metrics caused.

## Step 6: Report

When an experiment reaches an outcome (a winner at 100%, a loser, inconclusive, or a failed rollout), produce this report in the same reply, and set the record to "report produced":

```markdown
## Experiment Report: [Goal]

**Date:** [YYYY-MM-DD]
**Goal:** [metric] [direction] by [target]%
**Status:** [achieved / not achieved / inconclusive]

### Hypothesis
[What we tried and why]

### Change
- Flag: [flag key]
- Change: [as described in the handoff]
- Deployed: [who confirmed, and when]

### Experiment (10% of traffic)
- Status: [proven / loser / inconclusive / stopped for a guardrail]
- Duration: [time]
- Metric change: [control] → [treatment] ([+/-]%)
- Statistical significance: [value, confidence interval]

### Guarded rollout (if reached)
- Status: [completed / rolled back / not started]
- Duration: [time]
- Stages reached: [N of 3]
- If rolled back and retried: [cause, and the result of the retry]

### Safety metrics
- Error rate: [baseline] → [final] ([no regression / regression])
- [Other guardrails]: [baseline] → [final] ([status])

### Next steps
[What to do next. For a winner at 100%, recommend a cleanup change that removes the flag from the code and keeps the winning path, and then archiving the flag. For an experiment that did not win, recommend removing the flag from the code and keeping the current behavior, and then archiving the flag.]
```

## Safety rules

- Present the hypothesis, and get the person's agreement, before you create anything.
- Never treat a change as deployed without a person's confirmation.
- Always prove a change with a fixed 10% experiment before any guarded rollout.
- Run an experiment for at least 2 full weeks before you decide, unless a guardrail regresses.
- Always include an error-rate guardrail, separate from the success metric. Add a performance guardrail for a performance change.
- Set every rollout metric to both notify and roll back on regression.
- Keep every guarded rollout stage at or below 50%, and let LaunchDarkly promote to 100%.
- Tell a rollback that the metrics caused (do not retry) from one with an external cause (safe to retry once it is fixed).
- Change only flags with the tag `experiment-orchestration`, and keep the record in the flag description current.
