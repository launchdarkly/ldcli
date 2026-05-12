# Feature Research — `ldcli flags rollouts-beta`

**Domain:** CLI surface for managing automated rollouts (progressive + guarded) on top of LaunchDarkly's `automated-releases` API. First-class consumers: humans, CI/CD, and AI agents.
**Researched:** 2026-05-11
**Confidence:** HIGH for table-stakes / CLI-UX patterns (multiple concrete CLIs surveyed). MEDIUM for AI-agent-specific recommendations (industry guidance is recent, still consolidating).

## CLI Survey: How Other Tools Manage Rollouts

| Tool | `start` analog | `status` | `pause/stop` | `promote / continue` | `abort / rollback` | Watch / async | Health checks |
|------|----------------|----------|--------------|----------------------|--------------------|---------------|--------------|
| **`kubectl argo rollouts`** | `set image` (mutate spec to trigger rollout) | `status` (exits 0 when healthy, error otherwise) + `get rollout` (tabular detail) | `pause` | `promote` (next step) / `promote --full` (skip remaining steps + analysis) | `abort` (revert to previous ReplicaSet) + `retry` to restart aborted | `--watch / -w` on `get` and `status`; `--timeout` flag | `AnalysisRun` automatically pauses/aborts; `dryRun` metrics evaluate without affecting state |
| **`kubectl rollout`** | implicit (any `kubectl apply` triggers rollout) | `status deployment/foo` (blocks until healthy) | n/a (use `pause`) | n/a | `undo` (revert to previous revision) | `status` blocks by default; `history` for past revisions | n/a (vanilla k8s has no metric analysis) |
| **`gh pr checks`** | n/a (status surface only) | `gh pr checks` — bucketed states (pass/fail/pending/skipping/cancel) | n/a | n/a | n/a | `--watch` with `--interval` (default 10s) and `--fail-fast`; **exit code 8 = pending** | n/a |
| **`gh run watch`** | n/a | n/a | n/a | n/a | n/a | `--exit-status` to make exit code reflect run result; polls every 3s | n/a |
| **`spin` (Spinnaker)** | `pipeline execute` | `pipeline get`, `pipeline-executions get` | partial (via execution APIs, no dedicated CLI verb) | manual judgment via API | API only — no first-class CLI verb | Polling APIs documented; CLI doesn't ship rich watch | Canary lifecycle is API-driven; `canary` subcommand manages **configs**, not running canaries |
| **`siggy` (Statsig)** | `gates create` + `gates update` (replaces rules wholesale) | `gates list`, `gates check --user '<json>'` | n/a (toggle rule percentage) | n/a (set `passPercentage`) | n/a (set `passPercentage: 0`) | n/a — no rollout-orchestration primitive in CLI | n/a — Statsig CLI doesn't expose pulse/regression at CLI |
| **Flagsmith CLI** | scope is **read-only** (`flagsmith get`): fetches feature state for build pipelines | n/a | n/a | n/a | n/a | n/a | n/a |
| **Unleash** | No first-party CLI; API-first; some community CLIs exist | API-driven | API-driven | API-driven | API-driven | n/a | Some addons (e.g., Datadog) but not surfaced at CLI |
| **Optimizely CLI (`opti`)** | Project-config focused (sync experiments/flags between API and VCS), not rollout-execution focused | `opti list` style read | n/a | n/a | n/a | n/a | n/a |
| **LaunchDarkly `ldcli` today** | `flags create/update/toggle/archive` — flag CRUD + boolean toggle, no rollout primitive | `flags get` (resource-style read) | n/a | n/a | n/a | n/a (one-shot resource calls) | n/a |

### Patterns That Emerge

1. **Verb-noun shape converges.** Argo Rollouts uses `promote / abort / pause / retry / set / status / get`. `kubectl rollout` uses `status / undo / restart / history`. `gh pr` uses `checks / merge / review`. **Short, imperative verbs scoped under a noun.** Our shape `ldcli flags rollouts-beta <verb>` is consistent.
2. **Tabular human view + JSON machine view from the same command.** `gh pr checks --json bucket,state,name,...` and `kubectl ... -o json` are the dominant pattern. Bucketing/categorization (Argo's `Progressing` / `Healthy` / `Degraded` conditions, gh's pass/fail/pending bucket) is critical for agent consumption — it abstracts over many internal states.
3. **Watch is the dominant async pattern.** `--watch` flag with `--interval` and explicit `--timeout` is universal. **No tool surveyed exposes a "fire and forget, give me a job ID" pattern** — the rollout itself is the durable identifier; callers re-query it.
4. **Exit codes carry signal beyond "did the CLI run".** `gh pr checks` reserves exit 8 for "pending", `gh run watch` has `--exit-status` to opt into "exit reflects the watched run", `kubectl argo rollouts status` exits 0 on healthy / non-zero on degraded.
5. **Two-tier promote/abort vocabulary.** Argo's `promote` (next step) vs `promote --full` (skip everything) maps directly to our "stop rollout, finish to test variation" vs "stop rollout, revert to control" decision. Naming/finalization is the design lever.
6. **Health checks are upstream of "start" everywhere they exist.** Argo's `AnalysisRun` validates metric definitions before promotion; vanilla `kubectl` has nothing. **No surveyed CLI gates rollout creation on a metric pre-flight check** — that's a differentiator we can own for the AI-agent case.

## Feature Landscape

### Table Stakes (Users Expect These)

Missing any of these = product feels incomplete. All map directly to validated requirements in `.planning/PROJECT.md`.

| Feature | Why Expected | Complexity | Notes (incl. AI-agent angle) |
|---------|--------------|------------|------------------------------|
| `rollouts-beta start` with explicit options (env, stages, target variation, metrics opt-in, randomization unit, auto-rollback) | Maps to REQ-START-01/02/03; every comparable CLI has a "kick off a release" verb | MEDIUM | Agent angle: every option as a CLI flag (no required interactive prompts). Stages parsed from a single string or repeatable flag — needs deterministic ordering. |
| `rollouts-beta list <flag>` (current + past, deterministic ordering) | REQ-LIST-01; agents need to enumerate history without guessing IDs | LOW | Agent angle: stable sort order (e.g., reverse-chronological by createdAt, stable tiebreak by ID). Document the order. |
| `rollouts-beta status <flag>` — most-recent rollout, structured fields | REQ-STATUS-01/02; `gh pr checks` is the model | MEDIUM | Agent angle: bucketed top-level state (`running`/`paused`/`succeeded`/`failed`/`stopped`/`regression-detected`) on top of raw API status. Don't make agents pattern-match on enum churn. |
| Show stages, percentages, current stage, monitoring state, latest metric results | REQ-STATUS-02; this is the "rollout details" view users see in the UI | MEDIUM | Agent angle: nested JSON keyed by stable identifiers (metric key, stage index). No HTML/colors in JSON output. |
| `rollouts-beta stop <flag> --to-variation <key>` | REQ-STOP-01; Argo `abort` + `promote --full` are the precedents | LOW | Agent angle: `--to-variation` is required (no implicit default — agents shouldn't accidentally roll back when they meant forward). Idempotent if already stopped. |
| `rollouts-beta dismiss-regression <flag>` | REQ-DISMISS-01; LD UI surfaces a "Dismiss regression" affordance today | LOW | Agent angle: must be safe to call repeatedly (idempotent); return current regression state so agent knows what was dismissed. |
| `--output json` (and `--output plaintext`) | Existing ldcli convention; `gh`, `kubectl`, `argo` all do this | LOW | Reuse existing `internal/output/` infrastructure. JSON should default when stdout is not a TTY (already the ldcli convention per `cmd/root.go`). |
| `--project` / `--environment` / `--access-token` (persistent flags inherited) | Consistent with all other `ldcli` subcommands | LOW | No new auth surface (constraint per PROJECT.md). |
| Meaningful non-zero exit codes for failure modes | REQ-AGENT-01; `gh pr checks` exit-8-for-pending is the model | MEDIUM | See "Exit code contract" below. |
| `--help` produces complete, structured docs (Cobra default) | Cobra ships this; agents discover commands via `--help` | LOW | Verify every flag has a non-empty description (agents grep `--help` for capability discovery). |

### Differentiators (Competitive Advantage for AI-Agent UX)

These set the surface apart from every other rollout CLI surveyed.

| Feature | Value Proposition | Complexity | Notes (incl. AI-agent angle) |
|---------|-------------------|------------|------------------------------|
| **Default-fail on metric health-check problems with `--skip-health-checks` override** | REQ-START-04; no surveyed CLI does this. An agent will happily launch a rollout against mis-instrumented metrics today; this stops that. | MEDIUM | Health-check pre-flight runs before the rollout is created server-side. Surface specific failures (`metric-key`, `last-event-at`, `expected-events`) in JSON. In an interactive TTY: prompt. In a non-TTY (agent/CI): hard fail with a dedicated exit code unless `--skip-health-checks`. |
| **`--watch` mode that surfaces actionable events, not just terminal states** | REQ-STATUS-03; `gh pr checks --watch` only emits a refreshed bucket table. We emit discrete events ("regression detected", "stage advanced", "awaiting decision") so an agent can break out of watch loops and act — not just wait for "done". | MEDIUM-HIGH | Each event is one line of newline-delimited JSON (NDJSON) when `--output json`. Agent reads stdout line by line, reacts. Pretty plaintext for humans. Document event-type stability. |
| **Bucketed top-level status independent of API enum churn** | The `automated-releases` API is unstable; agents should not have to track every raw enum. The CLI translates raw API states into a small, stable bucket set in its JSON output. | LOW-MEDIUM | E.g., `{ "bucket": "regression-detected", "rawStatus": "monitoring_paused_due_to_regression", "actionRequired": true }`. Bucket names are versioned and slow-changing; raw status is exposed for debugging. Mirrors the `bucket` pattern in `gh pr checks --json`. |
| **Distinct, documented exit codes per failure class** | An agent's retry/escalation logic depends on `did this fail because of my input, the network, or a regression?` | LOW | Proposed table below ("Exit Code Contract"). Document in `--help` and in command output `meta` envelope. |
| **JSON envelope with `schemaVersion` and `apiVersion` fields** | The beta API will change shape. Versioning the CLI output schema lets agents pin/detect breaking changes without re-parsing the world. | LOW | E.g., `{ "schemaVersion": "1", "apiVersion": "automated-releases-2026-05", "data": {...}, "meta": { "warnings": [...] } }`. |
| **Deterministic ordering in all list output** | Agents diffing rollout state need stable order; tools like Argo and gh don't guarantee this consistently | LOW | Document the sort order (reverse chronological + ID tiebreak) in `--help` and in the JSON envelope. Same applies to nested stage arrays (must reflect API stage order, not Go map iteration order). |
| **Idempotent destructive commands (`stop`, `dismiss-regression`)** | Agents will retry on transient failures; double-stopping should be safe | LOW | If already stopped/dismissed, exit 0 with `"alreadyApplied": true` in JSON. Mirrors `kubectl apply` semantics. |
| **`--watch --timeout` with explicit timeout exit code** | `kubectl argo rollouts status --timeout 60s` is the precedent. Agents need a bounded watch — multi-day rollouts shouldn't tie up an agent process. | LOW | Distinct exit code for "watch timed out without terminal state" (so the agent can distinguish "still running, my watch expired" from "rollout failed"). |
| **`--watch --until=<event>`** filter (e.g., `--until=regression`, `--until=stage-advanced`, `--until=terminal`) | Agents want to wait for a *decision point*, not just completion | MEDIUM | `--until=terminal` is the default (succeeded/failed/stopped). Other values enable agent-driven escalation patterns. |
| **`rollouts-beta start --dry-run`** | Lets an agent (or human) validate the full request — env + metric availability + stages — without creating a rollout | MEDIUM | Returns the same JSON shape a real `start` would, with `"dryRun": true`. No surveyed flag CLI offers this; Argo's `lint` is the closest analog. |
| **Embedded "next-action hint" in JSON output** | An agent reading `status` shouldn't have to interpret enums to know what's possible. CLI tells it: `"availableActions": ["dismiss-regression", "stop"]`. | MEDIUM | Mirrors GitHub's `mergeable_state` + action affordances on PRs. Major DX win for agents. |

### Anti-Features (Deliberately NOT Building in v1)

| Feature | Why Tempting | Why Not Now | Alternative |
|---------|---------------|-------------|-------------|
| **Timeseries / chart data for metric results** | Mirrors the UI; "completeness" feels nice | Out of scope per PROJECT.md. Charts are a UI affordance; the CLI's job is the latest numeric value + verdict. Streaming timeseries blows up JSON output size and ties us to chart-rendering decisions. | Surface latest value + verdict + last-updated timestamp; link to UI URL for the chart in `meta.uiURL`. |
| **Release-policy-driven `start` defaults** | Lets `start` be a one-liner with no flags | Release policies are not yet GA. Building against them locks us in too early. | All v1 commands take explicit options. Once policies GA, add zero-arg `start` as additive enhancement. |
| **Configuring metric definitions or randomization units** | Feels natural to do "everything from CLI" | Pre-existing LD resources owned by experiments/metrics. Out of scope per PROJECT.md. Conflating creation with consumption inflates surface area. | Reference existing metrics/randomization units by key. Errors point at the UI/API to create them. |
| **A `notify-humans` / Slack-ping command** | Agents do want to escalate | Out of scope per PROJECT.md. Notification primitive is the agent's choice (open an issue, post to Slack, page someone). Hardcoding one mechanism is wrong. | Document agent-escalation recipes in README/examples; ship machine-readable `actionRequired: true` + `availableActions` so agents can plug into their own notify path. |
| **`pause` / `resume` as first-class verbs** | Argo Rollouts has these; users will ask | The `automated-releases` API conceptually doesn't expose a generic pause: monitoring pauses happen automatically on regression. Adding a CLI `pause` invents semantics the API doesn't have. | If the API later adds pause/resume, add verbs then. For now, "dismiss-regression to continue" + "stop" are the user-facing inflection points. |
| **A `promote` verb (separate from `stop --to-variation`)** | Argo's `promote --full` is iconic; users will reach for it | "Promote" implies a forward decision distinct from "stop". For LD, both forward (finish to test variation) and backward (revert to control) are *stops* of a running rollout with a variation choice. One verb keeps the mental model clean and matches the unified `stopAutomatedRelease` instruction. | Single `stop --to-variation <key>` covers both directions. Document mapping in `--help` ("`stop --to-variation <test>` is the equivalent of Argo's `promote --full`"). |
| **Cross-environment promotion workflow** (`promote --from staging --to production`) | Cross-env release management is a common ask | Out of scope per PROJECT.md; cross-env workflows are user composition. Building this in v1 expands scope and locks UX too early. | Document the pattern: agent runs `start --environment staging`, watches, then runs `start --environment production` with the same flags. |
| **Interactive TUI / wizard for `start`** | Quickstart already uses Bubbletea; rollouts feel similar | TUI optimizes for first-run human UX. Agents and CI/CD can't use it. Adding it bifurcates the codepath. | All required flags are explicit; if a user wants guided flow, they invoke `--help` and fill flags. (Could revisit post-v1.) |
| **Resource auto-generation from OpenAPI spec for rollouts** | Existing ldcli generator covers all REST resources | The `automated-releases` API is unstable and undocumented (per PROJECT.md). Auto-generation pulls in shifting shapes; agents see breakage. | Hand-written commands for v1; revisit once API is stable enough to be in the OpenAPI spec we generate from. |
| **Webhook/callback registration on rollout events** | "Reactive" rollouts are a natural ask | Out of scope; LD already has webhook infrastructure server-side. Duplicating that at the CLI is wrong layer. | Agents poll via `--watch`; LD's existing webhook product handles push semantics. |
| **A separate `metrics-health` subcommand** | Useful diagnostic | Folding it into `start` (as pre-flight) is the higher-value path. Standalone `metrics-health` is a follow-up if there's real demand. | `start --dry-run` exposes the same info. Standalone command becomes viable later. |
| **Streaming JSON output for non-watch commands** | "Consistent" with `--watch` NDJSON | One-shot commands should return a single, well-formed JSON object — easier to parse for agents that pipe into `jq`. | Single JSON object for `start`/`status`/`list`/`stop`/`dismiss-regression`. NDJSON only for `--watch`. |

### Future Opportunities (Out of Scope for v1)

| Feature | Why Out of Scope Now | When to Revisit |
|---------|-----------------------|------------------|
| Zero-arg `start` driven by release policies | Release policies not GA | When release policies ship |
| Auto-generated commands from OpenAPI spec | `automated-releases` API undocumented and unstable | When the API is in the published OpenAPI spec we already generate from |
| `pause`/`resume` verbs | API doesn't expose generic pause today | If/when the API adds them |
| Cross-environment promotion workflow | Users can compose v1 primitives | After v1 adoption signals what compositions are common |
| Interactive TUI for `start` | Optimizes for humans only; agents-first scope | After v1, if first-run friction is real |
| MCP server wrapper around the CLI | Direct MCP would shortcut the CLI surface | Once the CLI is stable; the CLI is itself a deterministic interface that an MCP server can wrap with low effort |
| Subscriptions/event-stream (server-push) replacement for `--watch` polling | API doesn't expose a stream yet | If the API adds an events endpoint |
| `rollouts-beta diff <id> <id>` | Comparing two rollouts is useful for postmortems | Post-v1; needs richer history modeling |

## Exit Code Contract (Proposed)

| Code | Meaning | Retryable? | Inspired by |
|------|---------|------------|-------------|
| 0 | Success (incl. idempotent no-op) | n/a | Universal |
| 1 | Generic CLI error | no | Universal |
| 2 | User input error (bad flags, malformed `--stages`) | **no** | Universal |
| 3 | Auth error (bad token, expired) | no (agent escalates) | Convention |
| 4 | Network / transient API error | **yes (backoff)** | Convention |
| 5 | Resource not found (flag/rollout/env) | no | Convention |
| 6 | Conflict (rollout already running on this flag/env) | no | Convention |
| 7 | Metric health check failed (and `--skip-health-checks` not set) | no — agent should investigate | New (CLI-specific) |
| 8 | Pending / not-yet-terminal (only for `--watch --timeout` expiring without terminal state) | yes (resume watch) | `gh pr checks` |
| 9 | Regression detected and rollout in `action-required` state — only emitted by `--watch` with the default `--until=terminal` if user opts into `--exit-on-regression` | conditional | `gh run watch --exit-status` analog |

Each non-zero exit also writes a machine-readable error envelope to stdout when `--output json`:
```json
{ "error": { "code": "metric-health-check-failed", "exitCode": 7, "details": {...} }, "schemaVersion": "1" }
```

## JSON Output Schema (Proposed Envelope)

```json
{
  "schemaVersion": "1",
  "apiVersion": "automated-releases-<date-or-id>",
  "data": { ... },
  "meta": {
    "uiURL": "https://app.launchdarkly.com/...",
    "warnings": [],
    "availableActions": ["stop", "dismiss-regression"],
    "fetchedAt": "2026-05-11T12:34:56Z"
  }
}
```

Why this shape:
- `schemaVersion` is the CLI's contract; bumped on breaking CLI output changes.
- `apiVersion` is the upstream LD API version the CLI was built against (lets agents detect drift independent of the CLI).
- `meta.availableActions` is the "next-action hint" differentiator — every command response that describes a rollout includes it.
- `meta.warnings` lets us deprecate flags/fields without breaking parsers.

## Watch Mode (NDJSON Event Schema)

When `--watch --output json` is set, the CLI emits **newline-delimited JSON objects** (one per event), then exits.

```json
{"event":"snapshot","rollout":{...},"at":"2026-05-11T12:00:00Z"}
{"event":"stage-advanced","fromStage":1,"toStage":2,"at":"..."}
{"event":"regression-detected","metricKey":"latency-p99","at":"..."}
{"event":"terminal","status":"succeeded","at":"..."}
```

Stable event names: `snapshot`, `stage-advanced`, `regression-detected`, `regression-dismissed`, `metric-update`, `action-required`, `terminal`. New events added under semver-minor; existing events never renamed within a `schemaVersion`.

## Feature Dependencies

```
start ──requires──> environment + flag + (optional) metric resources exist
  │
  └──depends on──> metric health-check pre-flight (REQ-START-04)
                       └──requires──> ability to look up metrics by key via LD API

status ──requires──> rollout exists on flag
  │
  └──enables──> --watch
                     └──requires──> NDJSON event schema
                     └──enables──> --until=<event>

stop  ──requires──> rollout exists AND is currently running
                     └──conflicts with──> a second concurrent stop call
                                             └──resolved by──> idempotent semantics

dismiss-regression ──requires──> rollout currently in regression-detected state
                                  └──idempotent if already dismissed

list ──independent──>  all of the above (read-only)
```

**Notes on key dependencies:**
- **Health-check pre-flight depends on metric lookups** — if the `automated-releases` API doesn't expose this directly, we may need a follow-up call to `/metrics/` for each referenced metric key. Document in API papercuts (REQ-DOC-01) if this is awkward.
- **`--watch` depends on a stable NDJSON event schema** — getting this right in v1 is high-leverage; renaming events later breaks agents.
- **`stop` and `dismiss-regression` must be idempotent** — without this, agents retrying on transient network errors cause confusing UX.

## MVP Definition

### Launch With (v1)

The minimum surface to satisfy every Active requirement in PROJECT.md:

- [ ] `rollouts-beta start` (REQ-START-01/02/03) — all options as flags, JSON request body accepted via `-d` for power users
- [ ] `rollouts-beta start --skip-health-checks` + default pre-flight (REQ-START-04)
- [ ] `rollouts-beta start --dry-run` (differentiator; cheap once pre-flight exists)
- [ ] `rollouts-beta list <flag>` (REQ-LIST-01) with deterministic ordering
- [ ] `rollouts-beta status <flag>` (REQ-STATUS-01/02) with bucketed top-level state
- [ ] `rollouts-beta status --watch [--interval] [--timeout] [--until]` (REQ-STATUS-03)
- [ ] `rollouts-beta stop <flag> --to-variation <key>` (REQ-STOP-01), idempotent
- [ ] `rollouts-beta dismiss-regression <flag>` (REQ-DISMISS-01), idempotent
- [ ] `--output json` envelope with `schemaVersion`, `meta.availableActions`, `meta.uiURL` (REQ-AGENT-01)
- [ ] Documented exit code contract (REQ-AGENT-01)
- [ ] Reuse existing `internal/output/`, `internal/resources/Client`, `cmd/cliflags/` (constraint)
- [ ] API-papercuts log being maintained throughout (REQ-DOC-01)

### Add After Validation (v1.x)

- [ ] `--exit-on-regression` for `--watch` (exit 9) — only after we see whether v1 NDJSON events suffice
- [ ] Richer `--until` predicates (e.g., `--until=stage>=3`, `--until=metric:latency-p99:regression`)
- [ ] Detailed metrics view subcommand (`rollouts-beta metrics <flag>`) if `status` JSON gets too dense
- [ ] Auto-generation of rollout commands from OpenAPI spec once API is documented

### Future Consideration (v2+)

- [ ] Zero-arg `start` when release policies GA
- [ ] `pause`/`resume` if API exposes them
- [ ] Cross-env promotion workflow if v1 adoption shows demand
- [ ] MCP server wrapping the CLI

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| `start` with full option surface | HIGH | MEDIUM (lots of flags, validation) | P1 |
| `status` with bucketed state + UI-parity fields | HIGH | MEDIUM | P1 |
| `--watch` with NDJSON events | HIGH (agent UX) | MEDIUM-HIGH (event taxonomy design) | P1 |
| `list` deterministic | HIGH | LOW | P1 |
| `stop --to-variation` idempotent | HIGH | LOW | P1 |
| `dismiss-regression` idempotent | HIGH | LOW | P1 |
| Metric health-check pre-flight + `--skip-health-checks` | HIGH (agent safety) | MEDIUM | P1 |
| JSON schema envelope + `availableActions` | HIGH (agent UX) | LOW | P1 |
| Exit code contract | HIGH (agent UX) | LOW | P1 |
| `--dry-run` | MEDIUM | LOW (free with pre-flight) | P1 |
| `--until` filters beyond `terminal` | MEDIUM | MEDIUM | P2 |
| `--exit-on-regression` | MEDIUM | LOW | P2 |
| Standalone `metrics-health` subcommand | LOW | LOW | P3 |
| `diff` between two rollouts | LOW | MEDIUM | P3 |

## Competitor Feature Comparison (key axes)

| Axis | Argo Rollouts | `gh pr checks` | Statsig (`siggy`) | Spinnaker (`spin`) | **Our v1** |
|------|---------------|----------------|-------------------|--------------------|--------|
| Start a rollout | Indirect (`set image`) | n/a | `gates update` | `pipeline execute` | **`rollouts-beta start` (explicit)** |
| Pre-flight metric validation | Server-side via `AnalysisRun` (post-start) | n/a | n/a | n/a | **CLI-side pre-flight before start (differentiator)** |
| Watch | `--watch` flag, table refresh | `--watch --fail-fast` | n/a | n/a | **`--watch` with NDJSON event stream + `--until`** |
| Exit codes for state | partial (status command) | exit 8 for pending | n/a | n/a | **Full documented contract incl. agent-specific codes (7, 9)** |
| Idempotency | partial | n/a | replace-rules-wholesale | n/a | **First-class on `stop` and `dismiss-regression`** |
| JSON envelope versioning | k8s `apiVersion` (transitive) | none | none | none | **`schemaVersion` + `apiVersion` (differentiator)** |
| Next-action hints | none | none | none | none | **`meta.availableActions` (differentiator)** |
| Cross-env workflow | n/a | n/a | n/a | yes (pipelines) | **Out of scope (compose v1 primitives)** |

## AI-Agent Consumption Checklist

These map to industry guidance ([Writing CLI Tools That AI Agents Actually Want to Use](https://dev.to/uenyioha/writing-cli-tools-that-ai-agents-actually-want-to-use-39no), [Building a CLI That Works for Humans and Machines](https://www.openstatus.dev/blog/building-cli-for-human-and-agents), [Keep the Terminal Relevant: Patterns for AI Agent Driven CLIs](https://www.infoq.com/articles/ai-agent-cli/)).

- [x] **Noun-verb tree** — `rollouts-beta <verb>` already in scope
- [x] **`--json` everywhere** — reuse existing `--output json`
- [x] **Self-describing `--help`** — Cobra ships this; verify completeness in QA
- [x] **Distinct exit codes** — see contract above
- [x] **Deterministic ordering** — documented sort order on `list` and nested arrays
- [x] **Schema-versioned output** — `schemaVersion` envelope
- [x] **Idempotent destructive ops** — `stop`, `dismiss-regression`
- [x] **Bounded long-running ops** — `--watch --timeout`, exit code 8
- [x] **Next-action hints** — `meta.availableActions`
- [x] **Pre-flight validation** — `--dry-run`, metric health-checks
- [x] **NDJSON for streams** — `--watch --output json` produces one JSON object per line

## Sources

- [kubectl argo rollouts CLI reference](https://argo-rollouts.readthedocs.io/en/stable/generated/kubectl-argo-rollouts/kubectl-argo-rollouts/)
- [kubectl argo rollouts promote](https://argo-rollouts.readthedocs.io/en/stable/generated/kubectl-argo-rollouts/kubectl-argo-rollouts_promote/)
- [kubectl argo rollouts status](https://argo-rollouts.readthedocs.io/en/stable/generated/kubectl-argo-rollouts/kubectl-argo-rollouts_status/)
- [kubectl argo rollouts abort](https://argoproj.github.io/argo-rollouts/generated/kubectl-argo-rollouts/kubectl-argo-rollouts_abort/)
- [Argo Rollouts Analysis & Progressive Delivery](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/)
- [kubectl rollout reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_rollout/)
- [gh pr checks manual](https://cli.github.com/manual/gh_pr_checks)
- [gh run watch manual](https://cli.github.com/manual/gh_run_watch)
- [gh exit codes](https://cli.github.com/manual/gh_help_exit-codes)
- [spin CLI guide](https://spinnaker.io/docs/guides/spin/)
- [spin pipeline management](https://spinnaker.io/docs/guides/spin/pipeline/)
- [Statsig CLI gate management](https://docs.statsig.com/statsigcli/gate-management)
- [Statsig CLI overview](https://docs.statsig.com/statsigcli/)
- [Flagsmith CLI documentation](https://docs.flagsmith.com/integrating-with-flagsmith/CLI)
- [Optimizely CLI repo](https://github.com/optimizely/optimizely-cli)
- [LaunchDarkly Guarded Rollouts docs](https://launchdarkly.com/docs/home/releases/guarded-rollouts)
- [LaunchDarkly Managing Guarded Rollouts (dismiss regression)](https://launchdarkly.com/docs/home/releases/managing-guarded-rollouts)
- [LaunchDarkly Progressive Rollouts docs](https://launchdarkly.com/docs/home/releases/progressive-rollouts)
- [LaunchDarkly Guardrail Metrics](https://launchdarkly.com/docs/home/metrics/guardrail-metrics)
- [LaunchDarkly CLI commands](https://launchdarkly.com/docs/home/getting-started/ldcli-commands/)
- [Writing CLI Tools That AI Agents Actually Want to Use](https://dev.to/uenyioha/writing-cli-tools-that-ai-agents-actually-want-to-use-39no)
- [Building a CLI That Works for Humans and Machines (OpenStatus)](https://www.openstatus.dev/blog/building-cli-for-human-and-agents)
- [Keep the Terminal Relevant: Patterns for AI Agent Driven CLIs (InfoQ)](https://www.infoq.com/articles/ai-agent-cli/)
- [Asynchronous Request-Reply Pattern (Microsoft)](https://learn.microsoft.com/en-us/azure/architecture/patterns/asynchronous-request-reply)
- `/Users/alex/code/launchdarkly/ldcli/.planning/PROJECT.md` (milestone scope and requirements)
- `/Users/alex/code/launchdarkly/ldcli/.planning/codebase/ARCHITECTURE.md` (existing CLI patterns)

---
*Feature research for: ldcli flags rollouts-beta milestone*
*Researched: 2026-05-11*
