# Pitfalls Research

**Domain:** AI-agent-friendly CLI consuming an unstable, undocumented backend API for automated feature flag rollouts
**Researched:** 2026-05-11
**Confidence:** HIGH (domain), MEDIUM (specific API behaviors — verified via prior LD ecosystem knowledge and best-practice sources, but the `automated-releases` API itself is undocumented; some assumptions need validation in Phase 1 spike)

---

## Critical Pitfalls

### Pitfall 1: Hidden coupling to internal API response shapes

**What goes wrong:**
The CLI parses fields like `latestStage.metrics[0].currentValue`, `monitoring.state`, or `release.status` directly into Go structs. When gonfalon renames `monitoring.state` → `monitoring.status`, removes a field, or changes a string enum value (`"regressed"` → `"regression_detected"`), the CLI breaks silently — either it returns wrong data, panics on nil pointer dereference, or worst case, *thinks* a rollout is healthy when it's regressed. Because this CLI is the *first* consumer of the `automated-releases` API, the API team has no reason to consider these renames breaking.

**Why it happens:**
- It's natural to model the response 1:1 with strongly-typed Go structs.
- Without contract tests against a recorded snapshot, drift only surfaces at runtime.
- The `-beta` suffix gives a false sense that the CLI is allowed to break with the API — but in practice, *users* of the CLI don't know when gonfalon shipped a breaking change.
- The codebase already has precedent: per `CONCERNS.md`, `cmd/resources/resource_cmds.go` is regenerated from OpenAPI but the drift check (`check-openapi-updates.yml`) runs only on a schedule and doesn't block PRs.

**How to avoid:**
- Define a **thin DTO layer** at the API boundary (e.g. `internal/rollouts/dto/`) separate from the domain model the rest of the CLI uses. Field renames touch one file.
- For *every* field the CLI reads, capture a **golden response fixture** in `internal/rollouts/testdata/` from a real API call. Add a contract test that fails loudly on schema drift (missing fields, type changes, removed enum values).
- Use Go's `json.Decoder.DisallowUnknownFields()` in **dev-mode contract tests** (not prod parsing) to detect new fields the CLI should consider exposing.
- Add a `--debug-raw` flag that prints the raw API response body alongside the parsed view, so an agent (or human) can compare what the API said vs. what the CLI showed when something looks off.

**Warning signs:**
- A field's zero value (`""`, `0`, `nil`) appearing in `--json` output where a populated value is expected — usually means the API field was renamed.
- A `status` enum value the CLI doesn't recognize being rendered as the empty string or a default.
- Tests passing locally but `ldcli` returning unexpected output in staging.

**Phase to address:**
Phase 1 (Spike / API exploration). Capture golden fixtures before writing any command. Reinforced in every subsequent phase that reads new fields.

---

### Pitfall 2: Misinterpreting unstable API error responses

**What goes wrong:**
The `automated-releases` API returns a 400 with a body like `{"code": "metric_unsupported_unit", "message": "..."}`. The CLI treats the message as user-facing prose and renders it verbatim. Then the API team:
- Changes the message wording (now reads "User cannot...") — the CLI now shows misleading text.
- Adds a new error code the CLI doesn't recognize and falls back to a generic "validation error" — the agent sees a non-actionable message.
- Returns a 422 instead of a 400 for the same condition — the CLI's error class dispatch breaks.

Worse: HTTP 5xx responses (gonfalon down, gateway timeout) get conflated with 4xx (user error) and an agent retries when it shouldn't, or escalates when retry would have worked.

**Why it happens:**
- The error contract is undocumented; the implementer infers it from one or two example responses.
- Error mapping is the part of the code with the least test coverage by default.
- Agents need *structured* errors (typed `code` field + recommended action) but the API may only return free-form `message` strings.

**How to avoid:**
- Build an **error taxonomy** in `internal/rollouts/errors.go` with explicit Go error types: `ErrMetricNotFound`, `ErrRolloutAlreadyRunning`, `ErrHealthCheckFailed`, `ErrSystemUnavailable`, etc. Each maps to a stable CLI exit code and a structured `--json` error object.
- For every API error code observed during development, **document it in `.planning/API-PAPERCUTS.md`** and map it explicitly. Unmapped codes get a sentinel exit code (`70` "unknown upstream error") so agents can detect "this is new, escalate."
- **Never** include the raw upstream `message` field as the sole signal in `--json` mode. Always pair it with a stable `error.code` field owned by the CLI (not the API).
- Distinguish **transport** errors (network, 5xx, timeout) from **business** errors (4xx with structured body) — different exit codes, different agent-recommended actions.

**Warning signs:**
- A new error message appearing in CLI logs that doesn't match any handled case.
- Agents reporting "I got 'validation failed' but I don't know which input was wrong."
- Retries succeeding for what was reported as a permanent failure (means transient/permanent classification is wrong).

**Phase to address:**
Phase 2 (Start command) — first place errors must be classified. Refined as new commands surface new error shapes.

---

### Pitfall 3: Building UX around fields that will be renamed/removed

**What goes wrong:**
A flag like `--stage-duration` directly maps to a request body field `stages[].duration`. Later the API renames it to `stages[].window` or splits it into `holdMinutes` + `rampMinutes`. The CLI has shipped to users; renaming the CLI flag is now a breaking change. The team is stuck supporting both names indefinitely, or worse, the flag silently does nothing because the request body still uses the old field name.

**Why it happens:**
- 1:1 mapping between CLI flag names and API field names is the path of least resistance.
- The undocumented API has no deprecation policy; renames can happen at any release.
- The `-beta` suffix is on the command tree, not the individual flags — users (and agents) reasonably assume flag names are stable.

**How to avoid:**
- **CLI flag names belong to the CLI**, not the API. Pick names that describe user intent (`--stage-duration`, `--target-variation`, `--rollback-on-regression`) and map them to whatever the API currently calls them in an explicit translation layer.
- When a flag name *must* match an API concept, choose the most invariant name (e.g. `--metric-key` is safer than `--metric-id` because LD uses "keys" everywhere).
- Resist exposing every API knob as a flag. Each flag is a long-term commitment; opt for sensible defaults and add flags lazily based on real user requests.
- Document the **CLI-to-API name translation** in a single file. Renames there should be one-line changes.

**Warning signs:**
- Pull requests that simultaneously rename an API field and a CLI flag — strong signal of leaky abstraction.
- "We can't change the flag because users depend on it, but the API field is deprecated."
- Multiple flags being added at once because "the API has these fields" rather than "users asked for them."

**Phase to address:**
Phase 2 (Start command) — most flag surface. Apply the same discipline in Phase 3 (Stop/Dismiss) and Phase 4 (Status).

---

### Pitfall 4: Losing track of which API decisions were workarounds vs. intentional

**What goes wrong:**
Six months into the project, the team can't remember:
- Why the CLI sends `randomizationUnit: "user"` even when the user didn't specify it (was that a temporary workaround for an API 500, or intentional UX?)
- Why metric health check polling has a 2-second sleep before reading results (was the API eventually-consistent at one point, or is that still needed?)
- Why we silently drop `monitoring.windowStart` from the JSON output (was the field unreliable, or did we decide it wasn't useful?)

When the API stabilizes and these workarounds are no longer needed, no one knows which to remove. They calcify into permanent technical debt.

**Why it happens:**
- Workarounds get committed in PRs with messages like "fix flake" or "handle edge case."
- Code comments say *what* the workaround does, not *why*, and don't get updated when the API changes.
- `.planning/API-PAPERCUTS.md` is intended to capture this but easily devolves into a brain-dump that no one reads.

**How to avoid:**
- Every workaround in code carries a `// PAPERCUT: <one-line summary>` comment with a link/anchor to the section in `API-PAPERCUTS.md`.
- **`API-PAPERCUTS.md` structure** (avoid brain-dump):
  - Each entry has a **stable anchor ID** (`PC-001`, `PC-002`...).
  - Required fields per entry: `Title`, `Discovered` (date + commit), `API behavior`, `CLI workaround`, `What we'd prefer`, `Status` (active / fixed in API / superseded), `Removal criteria` (the specific signal that means the workaround can be deleted).
  - One section per entry; max ~10 lines each. If it's longer, it's a design doc, not a papercut.
  - Top of file: a one-page "active workarounds" index — anchor + one-line description.
- When the API team fixes something, the entry moves to a `## Resolved` section with the resolution date. The workaround code is then deleted in the same PR that updates the papercut entry.
- A periodic (per-phase-transition) review of `API-PAPERCUTS.md`: anything in "active" longer than 90 days gets an explicit owner or is escalated.

**Warning signs:**
- A `// TODO: remove this when the API is fixed` comment older than 6 months without an issue link.
- The team disagreeing about whether a behavior is a workaround or intended.
- An API team member fixing something and the CLI not picking it up because no one knew the workaround was tied to it.

**Phase to address:**
Phase 1 (Spike) — establish papercut format and a starter entry or two. Enforced via PR review template in every subsequent phase.

---

### Pitfall 5: `--watch` that never terminates or misses transitions

**What goes wrong:**
Two failure modes, both common:

**(a) Never exits:** The CLI polls `/automated-releases/{id}` every 5s. The API never reports a terminal state — either because the rollout was paused upstream, the user manually intervened in the UI, or there's an undocumented intermediate state the CLI doesn't recognize as terminal. The watch hangs forever, an agent's session times out, or worse, a CI job consumes a runner indefinitely.

**(b) Misses transitions:** The rollout transitions `running` → `regression_detected` → `rolled_back` within 2 seconds, between two polls. The CLI's next poll reads `rolled_back` and reports "rolled back" — but never surfaces the regression, which is the most important event for an agent to react to. (This is the real lesson from `gh pr checks --watch` per [cli/cli#7401](https://github.com/cli/cli/issues/7401) and [cli/cli#463](https://github.com/cli/cli/issues/463): race conditions on initial state and intermediate transitions are real.)

**Why it happens:**
- Watch is often implemented as "poll until status is in a hard-coded terminal set" — incomplete sets and unknown states both break this.
- Polls return only current state, not events. The CLI sees "what is now," not "what happened."
- Network blips and 503s can pause the watch without the user noticing, then the watch resumes with stale local state.

**How to avoid:**
- **Two-track watch logic**:
  1. **Current-state poll** for periodic display ("here's where the rollout is now").
  2. **Transition detection** by diffing against the previously-fetched state. Surface every diff that crosses an "actionable" boundary (regression detected, stage advanced, monitoring window opened, action required). Do not rely on the API reporting "the previous state was X."
- **Hard watch timeout** by default (e.g. 4 hours), with `--watch-timeout 0` to disable. Print a clear message on timeout: "Watch timed out after 4h; rollout still running. Re-run `ldcli flags rollouts-beta watch <id>` to resume." Exit code distinguishes "watched to terminal state" (0) from "watch timed out" (75) from "watch saw regression" (10).
- **Exit on the first actionable event by default** (modeled on `gh pr checks --watch`), with `--watch-until terminal` to wait for completion. An agent's mental model is "watch until something I need to decide about happens, then return control to me."
- **Treat unknown status values as fatal-for-watch**, not as "keep polling." Surface them, exit with a distinct code, document them in `API-PAPERCUTS.md`. This is how the CLI discovers new states early.
- **Resumability:** the watch's only state is "what was the last status I saw." On Ctrl-C or network failure, exit cleanly with that status printed so the user can re-invoke from where they left off. Don't try to persist watch state to disk.

**Warning signs:**
- Issues filed: "ldcli watch hangs after my rollout completed."
- Agents reporting "the rollout was rolled back but I didn't get a regression event."
- The watch's terminal-state set being updated reactively after each new gonfalon release.

**Phase to address:**
Phase 4 (Status / Watch). Critical path; this command is the agent's primary feedback loop.

---

### Pitfall 6: Watching a multi-day rollout continuously is the wrong UX

**What goes wrong:**
A progressive rollout has stages of "20% for 24h, 50% for 24h, 100% for 24h." A naive agent runs `ldcli ... watch --watch-until terminal` and burns 3 days of session time / token budget polling. Even with backoff, an agent's context window can't usefully hold 72 hours of "no change" output. CI runners cost money. Watch is fundamentally the wrong tool for multi-day duration.

**Why it happens:**
- `--watch` is the obvious answer to "how do I know when this is done?"
- The CLI doesn't make explicit that multi-day rollouts are common.
- There's no obvious alternative to watch in the existing design.

**How to avoid:**
- **Watch defaults to "until next actionable event"**, not "until terminal." The agent gets control back when something needs deciding, then re-invokes if it wants to keep watching.
- **`ldcli flags rollouts-beta status`** without `--watch` is the recommended path for "check in periodically." Agents poll the *CLI* on their own schedule (e.g. via cron, GitHub Actions schedule, an LLM running once an hour). The CLI doesn't try to be a long-running daemon.
- Estimate and surface the rollout's expected duration in `status` output (`Expected completion: ~2026-05-14T10:00:00Z based on configured stage durations`) so agents know when re-polling is sensible.
- Document in `--help` and command output: "For rollouts longer than a few hours, prefer scheduled `status` checks over `--watch`."

**Warning signs:**
- Watch processes running for >24h in CI logs.
- Agents that "lose track" of rollouts because they were watching one and the session ended.

**Phase to address:**
Phase 4 (Status / Watch). Communicated in command help and the public README in Phase 6 (polish).

---

### Pitfall 7: Output that looks machine-readable but is fragile

**What goes wrong:**
The CLI outputs:
```
Rollout abc123: running (stage 2 of 4, 50%)
  Latest metric: error_rate = 0.023 (baseline: 0.019) ✓
  Started: 3 hours ago
  Next stage: in 21 hours
```
An agent tries to parse this. Things that break parsing:
- "3 hours ago" — not a timestamp; nondeterministic relative time.
- `✓` — a Unicode glyph that may render differently or get stripped.
- ANSI color codes in the metric line leaking into stdout when piped (`isatty` check missing or wrong).
- Column ordering changes between releases.
- "stage 2 of 4" — requires regex parsing to extract `current_stage=2, total_stages=4`.

Worse: an agent that consumes `--json` output assumes the schema is stable, but a refactor of the underlying domain model silently reorders or renames a JSON field.

**Why it happens:**
- Human-friendly output is the natural starting point; JSON is added later as an afterthought.
- ANSI codes are added per-line ("`color.Green(...)`") without a global "is this terminal?" gate.
- Timestamps are formatted with `time.Since(...).String()` because it reads nicely.
- The JSON schema is "whatever struct serializes" rather than an explicit contract.

**How to avoid:**
- **Two separate output paths** from the start, not "human first, JSON bolted on":
  - `output/human.go`: ANSI codes, relative times, friendly formatting. *Only* used when stdout is a TTY and `--output` is not set to `json`.
  - `output/json.go`: structured DTO with **explicit field names** declared in code (not just `json:"..."` tags on a domain struct). Timestamps as RFC3339 UTC. Enums as documented strings. No `omitempty` on fields agents need to detect "missing."
- **ANSI-codes-on-stdout test**: pipe stdout to a buffer and assert no `\x1b[` sequences appear when `--output json` or when stdout is not a TTY.
- **`--output json` is honored by every command, including errors**. An agent should never have to parse stderr text.
- **Document the JSON schema** in `docs/json-schema/` (or as Go-generated JSON Schema files) and treat schema changes as breaking — even with the `-beta` command suffix.
- Forbid emoji and Unicode glyphs in JSON output. Use enum strings (`"status": "healthy"`) not `"✓"`.

**Warning signs:**
- A bug report: "my agent broke when I upgraded ldcli — the timestamp format changed."
- `\x1b[` characters in CI logs that were piped through `ldcli ... | jq`.
- A `--json` output that includes "3 hours ago" or relative times.
- A field present in some responses and absent in others without documented reason.

**Phase to address:**
Phase 0 (Foundations) or Phase 1 (Spike) — output contract has to exist before commands are written. Verified in every command's tests in subsequent phases.

---

### Pitfall 8: Exit codes that conflate failure modes

**What goes wrong:**
The CLI exits 1 for everything: user input error, network error, regression detected, rollout already running, auth failure. An agent receiving exit code 1 has to scrape stderr to decide what to do. It misroutes:
- "Already running" (should silently no-op or report status) becomes "fail the CI job."
- "Regression detected by watch" (should escalate to a human) becomes "retry the command."
- "Auth expired" (should re-auth) becomes "fail and alert."

The existing `ldcli` codebase mostly returns Cobra's default exit 1 for any error, so this is the path of least resistance.

**Why it happens:**
- Go's idiomatic `return err` from a Cobra `RunE` collapses everything to exit 1.
- Exit code design feels like a niche concern until an agent depends on it.
- It's easy to add new error paths without classifying them.

**How to avoid:**
- **Define an exit code contract early** and treat it as a public API:
  | Code | Meaning | Agent action |
  |------|---------|-------------|
  | 0 | Success | Continue |
  | 1 | General/unclassified error | Investigate stderr |
  | 2 | Usage error (bad flags, missing required input) | Fix invocation, don't retry |
  | 3 | Resource not found (flag, env, rollout doesn't exist) | Check inputs, don't retry |
  | 4 | Auth/permission denied | Re-auth, then retry |
  | 5 | Conflict (rollout already running, flag is off, etc.) | Check state, decide |
  | 10 | Regression detected (from watch) | Escalate / decide on dismiss vs. rollback |
  | 11 | Health check failed | Fix metric instrumentation, retry |
  | 70 | Unknown upstream error code (API returned something CLI doesn't classify) | Escalate; CLI may need an update |
  | 75 | Watch timeout (rollout still running) | Re-watch or status-poll |
  | (others reserved for future use)
- Every Cobra `RunE` returns a typed error that maps to one of these codes via a central `cmd/exit.go`.
- **`--json` errors include the exit code as a field** so agents can decide without checking `$?` separately.
- Document the exit code contract in `--help` and the public README.

**Warning signs:**
- Agents writing `if [ $? -eq 1 ]; then grep stderr for "..."; fi`.
- New error paths added without a corresponding exit code assignment.
- Same exit code returned for both "you can retry this" and "you cannot retry this" cases.

**Phase to address:**
Phase 0 (Foundations) — set the contract. Audited in every command-introducing phase.

---

### Pitfall 9: Silent fallbacks and mutations without acknowledgment

**What goes wrong:**
- `ldcli flags rollouts-beta start --env production --metric latency` — the metric `latency` doesn't exist. The CLI helpfully suggests `latency_ms` and uses it. The agent didn't authorize that substitution.
- Start succeeds; the CLI prints `OK` and exits 0. The agent has no rollout ID, no link to view it in the UI, no way to confirm exactly what it just did.
- The user passes `--stage 25,50,75,100` but the API only supports 4 stages of fixed durations; the CLI silently coerces to `25,50,75,100` over 1 hour each.

**Why it happens:**
- Helpful defaults and corrections feel UX-friendly for humans.
- "Worked, exit 0" is the path of least resistance for the success path.
- For an agent, *any* silent transformation is a trust-breaking event.

**How to avoid:**
- **No silent substitution.** If the agent says `--metric latency` and that's invalid, error with exit code 3 and a structured error suggesting `--metric latency_ms`. Don't auto-correct.
- **Every mutation prints a confirmation** in both human and JSON mode containing: the resource ID created/modified, a permalink to the UI, the *effective* parameters the API accepted (which may differ from what the agent supplied — make differences explicit).
- **`--dry-run` for `start` and `stop`** — validate the request against the API (or local rules) without mutating; print the effective request that *would* be sent. Agents run dry-run first when uncertain.
- For unavoidable normalization (e.g. the API stores durations as seconds but the CLI accepts `1h`), echo back the normalized value in the response (`effective_duration: "3600s"`).

**Warning signs:**
- Agents reporting "I ran start and don't know if it worked."
- The CLI saying "Created rollout (used metric `latency_ms` instead of `latency`)" rather than failing.
- Discrepancies between flags passed and flags shown in the resulting status output.

**Phase to address:**
Phase 2 (Start), Phase 3 (Stop/Dismiss).

---

### Pitfall 10: Authentication scope creep

**What goes wrong:**
The CLI reuses the user's existing LD access token (per the constraint). That token may have wide scope — write access to projects, members, billing. An agent running `ldcli flags rollouts-beta start ...` does so with the user's full permissions. If the agent is compromised or makes a mistake, the blast radius is "everything the user can do," not "manage rollouts on this flag."

Additionally, the token is stored in plaintext YAML (per `CONCERNS.md`) with no file permission enforcement. An agent that exfiltrates the config file gets a token with broad scope.

**Why it happens:**
- The existing auth surface (`internal/login/`) is reused per constraint; designing new scoping is out of scope for this milestone.
- Token scoping is an LD platform concern, not a CLI concern — easy to hand-wave.
- It's tempting to feature-flag rollout commands as just "another resource" without considering they're a high-impact write operation.

**How to avoid:**
- **Document the required token scope** in `--help` for rollout commands. The minimum is `write:flags` on the targeted project/env; surfacing this lets users create scoped service tokens.
- **Detect insufficient scope early**: on `start`, if the API returns 403, exit 4 with a message naming the specific scope/role the agent needs.
- **Recommend service tokens, not personal tokens, for agent use cases** in documentation. Service tokens can be scoped to a project; personal tokens can't be scoped at all.
- Do **not** add new auth surface; do **not** widen the scope that the CLI requests.
- File system permission: when `ldcli config` writes the token, `chmod 0600` (already a documented concern in `CONCERNS.md`; not strictly in scope here but worth a one-line fix-or-flag).

**Warning signs:**
- The CLI uses an API endpoint that requires more scope than rollout management would suggest (e.g. `read:members`).
- Documentation says "use your personal access token" without scope guidance.
- 403 errors getting a generic "permission denied" message without scope context.

**Phase to address:**
Phase 2 (Start) — first mutation that needs write scope. Re-validated in Phase 3.

---

### Pitfall 11: Race conditions with UI-initiated changes

**What goes wrong:**
The agent runs `ldcli ... start --metric latency_p99`. Two seconds later, a human stops that rollout in the UI. The agent then runs `... dismiss-regression`. The API returns 404 or worse, 200 with an undocumented body. The agent now has stale state and can make wrong decisions: "regression dismissed, rollout continues" when really the rollout was stopped 3 minutes ago.

Also: two agents racing on the same flag. Both check "is a rollout running?" — both see no, both call `start`. The API may accept both, may reject one, may produce two parallel rollouts (undocumented).

**Why it happens:**
- The CLI's mental model assumes it's the only writer.
- LD's UI doesn't (and shouldn't) lock a flag during automated rollout management.
- Idempotency keys may or may not be supported by the `automated-releases` API; assumption is unverified.

**How to avoid:**
- **Every mutation operates on a specific rollout ID, not just a flag.** `dismiss-regression --rollout-id abc123` — if that rollout is no longer running, the API returns a clean 404 / 409 and the CLI exits with code 5 (conflict) or 3 (not found) with a clear message.
- **`start` returns a rollout ID** immediately; agents pass that ID to subsequent commands rather than re-deriving from "the latest rollout on this flag."
- **Verify rollout state before destructive operations**: `stop --rollout-id abc123` first GETs the rollout. If status is already terminal, exit 5 with "rollout already finished as <status>." Don't blindly POST stop.
- **Idempotency**: if the API supports an `Idempotency-Key` header, send one (a UUID per command invocation). If it doesn't, document it as a papercut.
- **Concurrent-start protection**: `start` should fail with exit 5 if any rollout is already running on the flag, with a clear "use --force to override" escape hatch (which should be rare).

**Warning signs:**
- A `dismiss-regression` call returning 200 on a flag that no longer has an active rollout.
- Two `start` invocations within seconds both returning success and different IDs.
- The CLI's "latest rollout" lookup returning a rollout the user already stopped.

**Phase to address:**
Phase 2 (Start), Phase 3 (Stop/Dismiss).

---

### Pitfall 12: Metric/randomization-unit mismatch failing mid-rollout

**What goes wrong:**
At start, the API accepts a rollout with `randomizationUnit: "user"` and `metric: "checkout_latency"`. The metric is actually instrumented on `randomizationUnit: "org"`. The start succeeds because validation is lazy. At stage 1 transition (24h later), the API tries to evaluate the metric, gets no data, and either fails the rollout, hangs in an undocumented state, or proceeds with bad data. The agent that started the rollout is long gone.

**Why it happens:**
- The API may not validate metric/unit compatibility at start time (validation is hard at create-time when metric data hasn't been collected yet).
- Pre-flight checks per `REQ-START-04` are the right mitigation, but they have to be deep enough to catch this.
- "Validation passed at start" is a brittle promise — health doesn't just degrade, it can have been broken all along.

**How to avoid:**
- **Pre-flight health checks check unit compatibility**: fetch the metric definition, fetch the rollout's configured randomization unit, fail if they don't match. Don't rely on the API to do this.
- **Pre-flight health checks also include "has this metric received events recently"** (e.g. in the last 24h). A metric that exists but has no data is a common silent failure.
- **Surface different health check failures as distinct exit codes / error types** so agents can route. `health_check_failed.metric_unit_mismatch` is different from `health_check_failed.metric_no_recent_events` — both block start, but tell the agent different things to fix.
- Document each health check rule in `--help` for `start`. If a user runs `--skip-health-checks`, the output prints which specific checks would have failed so the agent has the record.

**Warning signs:**
- Rollouts that successfully start but stall at the first stage transition.
- Health check passes followed by a regression event with no metric data backing it.
- Different metric kinds (numeric, conversion, count) needing different health checks but getting the same one.

**Phase to address:**
Phase 2 (Start) — health checks are core to the start UX per `REQ-START-04`.

---

### Pitfall 13: Health checks themselves are slow or block the CLI

**What goes wrong:**
Pre-flight health check fetches metric definition (1 API call), recent events (1 call), randomization unit (1 call), and event rate (1 call) — sequentially. Total: 4 round-trips, ~3-8 seconds. Now the `start` command always takes ~10s. CI pipelines slow down; agents time out. Users start passing `--skip-health-checks` to "make it fast" and bypass the safety net entirely.

**Why it happens:**
- Health checks are added as a feature, not a perf-conscious design.
- "It's only a few seconds" is true in isolation but doesn't account for `start` being invoked at scale (every PR merge, etc.).
- The `--skip-health-checks` escape valve becomes the default if checks are annoying.

**How to avoid:**
- **Parallelize health check API calls** with `errgroup` — sub-second total.
- **Cap total health check time** with a context deadline (e.g. 5s). If checks don't complete in time, fail with a specific exit code (`health_check_timeout`), don't proceed silently.
- **Make health checks streamable**: print each check as it completes (in human mode) so the user sees progress, not a 5-second silence.
- **For `--json` mode**, emit a structured `health_checks` array in the success response so agents can audit which checks ran and what their results were — even on success.
- Don't add new health checks without measuring the perf budget for `start`.

**Warning signs:**
- `start` taking more than 2-3 seconds in the success case.
- Users routinely passing `--skip-health-checks`.
- Adding a new health check fixing one bug while regressing latency for everyone else.

**Phase to address:**
Phase 2 (Start) — health checks are introduced there.

---

### Pitfall 14: "Dismiss regression" semantics when the rollout is already over

**What goes wrong:**
The agent runs `ldcli ... dismiss-regression --rollout-id abc123` 30 seconds after a regression event. But in those 30 seconds the auto-rollback triggered and the rollout is now in `rolled_back` state. The API may:
- Return 200 OK (idempotent-ish, dismiss is a no-op) — the agent thinks it succeeded and the rollout continued.
- Return 409 with an unhelpful message.
- Return 200 and *resume* the rollout in some half-state.

The undocumented API makes all three plausible until verified.

**Why it happens:**
- Dismiss is naturally async vs. the rollout's progression.
- The API may not have a clear "this rollout is in a state where dismiss is meaningful" rule.
- Agents may dismiss based on stale state from a watch event.

**How to avoid:**
- **`dismiss-regression` reads the rollout state first.** If not in a state where dismiss is meaningful (e.g. already rolled back, already completed, no active regression), exit 5 (conflict) with a structured error that names the current state. Don't issue the POST.
- **Pair dismiss with an expected-state header or body field** if the API supports optimistic concurrency. If not, document it as a papercut and request the API team add it.
- **`--json` response from dismiss must include the post-dismiss state** so the agent confirms the dismiss actually changed something.
- Document in `--help`: "If the rollout has already auto-rolled-back, dismiss is not possible. Use `start` to begin a new rollout."

**Warning signs:**
- `dismiss-regression` returning 0 on terminal-state rollouts.
- An agent dismissing a regression and then watching the rollout continue to roll back anyway.
- Different API behavior between "dismiss while regression is active" and "dismiss after rollback" without distinct error codes.

**Phase to address:**
Phase 3 (Stop/Dismiss).

---

### Pitfall 15: Starting a rollout on a flag in an incompatible state

**What goes wrong:**
The agent runs `ldcli ... start` on:
- A flag that's currently **off** in the environment → may succeed at the API level but the rollout has no effect; the agent thinks something is rolling out when nothing is changing.
- A flag that already has a **manual targeting rule** for the target variation → the rollout's effect is masked.
- A flag that's a **boolean** when the agent's metric is conversion-based and expects 3+ variations.
- A flag that already has an **active rollout** (covered in pitfall 11).

**Why it happens:**
- The API may permit operations that don't make semantic sense.
- Flag state is a complex multi-dimensional object; "is this a valid place to start a rollout?" has many sub-questions.
- The CLI focuses on the rollout API surface and doesn't think about the flag's overall state.

**How to avoid:**
- **Pre-flight flag state checks** (separate from metric health checks): is the flag on in the target env? Does it have conflicting rules? Is the target variation valid? Use these to fail-fast with structured errors.
- **Exit codes / error types differentiate** flag-off (`flag_disabled`), conflicting-rules (`flag_has_targeting_conflict`), invalid-variation (`flag_variation_not_found`).
- Default-fail in non-interactive mode; document `--force` escape valves with explicit warnings.
- Reuse existing `internal/flags/` client to check flag state — don't re-implement.

**Warning signs:**
- Rollouts "completing" with no observed effect on user behavior.
- Users discovering days later that their flag was off the whole time.
- The CLI happily starting rollouts that the LD UI would warn against.

**Phase to address:**
Phase 2 (Start).

---

### Pitfall 16: First-consumer assumptions baked into the codebase

**What goes wrong:**
Throughout the code, assumptions that hold for "this CLI" but not for "any future client" creep in:
- Hard-coded list of stage durations because "we know the API only supports 1h, 4h, 24h."
- Assumption that `monitoring.state` is always one of 4 known values.
- A request that always sends `clientVersion: "ldcli"` because "we're the only consumer."
- Workarounds for API behavior the team only observed once.

Six months later, the API team ships a second consumer (a different CLI, a Terraform provider). The CLI's assumptions force the API team to maintain undocumented behavior because changing it would break the CLI.

**Why it happens:**
- Being first consumer means the API team's behavior is partly defined by what the CLI does.
- It's hard to distinguish "this is how the API works" from "this is how the API works *for us, right now*."
- The CLI's `-beta` posture creates a false sense that assumptions are cheap.

**How to avoid:**
- When making an assumption, ask: "would this assumption be true for a hypothetical second consumer who doesn't know about us?" If no, document it as a papercut.
- Send a `User-Agent` header with `ldcli/<version>` — so the API team can see usage. Don't send anything else identifying.
- Don't lobby the API team to preserve undocumented behavior. If the CLI broke when an internal API change shipped, fix the CLI; surface the change as a public papercut for the API team to consider.
- Periodic (per-phase) review: which behaviors does the CLI rely on that aren't in the (eventual) public docs?

**Warning signs:**
- A papercut entry that's been "active" for 6 months because "the API team can't change it, it would break us."
- API team members asking "how does the CLI use field X?" — that's a sign of bidirectional coupling.
- The CLI's request shapes differing from what the API team thinks the request shapes should be.

**Phase to address:**
Phase 1 (Spike) — surface assumptions early. Re-audited at each phase transition.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Map API response struct directly to CLI domain model (no DTO layer) | Less code | Every API field rename is a CLI breaking change | Never for the rollouts subtree |
| Skip JSON schema documentation; "JSON is whatever the struct serializes" | Faster iteration | Agents have no stable contract; schema changes silently | Only in Phase 1 spike; documented before Phase 2 ships |
| Reuse existing global Viper for new flags | Consistent with existing pattern | Inherits the parallel-test-unsafe debt from `CONCERNS.md` | Acceptable for this milestone; flag as a follow-up |
| Hard-code well-known stage durations as flag choices | Validation is trivial | Breaks when API adds new options; flag is a lie | Never; accept any duration string and let API validate |
| Use `cmd/resources/` generated commands as the surface for rollouts | "Free" commands from OpenAPI | The `automated-releases` API is unstable; regen churn would be high; UX wouldn't fit a generated mold | Never for v1; revisit if API stabilizes |
| Single error type with a string message | One Go type to define | Agents can't route on error; exit codes can't be distinct | Never |
| `--watch` polls a single endpoint and looks at status only | Easy to implement | Misses transitions; can't surface regressions reliably | Never — diff-based transition detection is required from day one |
| Hard-code rollback decision logic in CLI ("if regression, auto-stop") | "Helpful" automation | Agent's prerogative; the CLI should report, not decide | Never; the API auto-rollback is the only auto-action |
| `--skip-health-checks` is honored without echoing what would have failed | Cleaner UX | Agents lose audit trail of what was skipped | Never — always print skipped check names in success output |

---

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `automated-releases` REST API | Treat `400 Bad Request` body as user-facing prose | Parse `code` field into a Go error type; the `message` is debug-only |
| `automated-releases` REST API | Assume `monitoring.state` enum is closed | Treat unknown enum values as fatal-for-watch, surface as exit 70 |
| `startAutomatedRelease` / `stopAutomatedRelease` flag patch instructions | Send the patch directly instead of through the flag-update infrastructure | Use the same flag-patch path the existing CLI uses; document if the patch instructions need flag versioning headers |
| LaunchDarkly access token | Assume personal access token is fine for agent workflows | Recommend (and document) service tokens with scoped roles for non-interactive use |
| LaunchDarkly metric definitions API | Fetch metric, see it exists, assume it's compatible with rollouts | Verify randomization unit, recent event volume, metric kind — all distinct checks with distinct error codes |
| `--json` consumers (jq, agents) | Print human-formatted strings ("3 hours ago", "2.5 GB") in JSON fields | RFC3339 timestamps; numeric fields stay numeric; durations as ISO 8601 or seconds |
| Existing `internal/flags/` Client | Reach for the auto-generated `resources/` commands first | Use the typed `Client` interface in `internal/flags/` for testability; only use generated commands for resources without custom logic |
| Cobra `RunE` error handling | Return any `error`, get exit 1 | Map errors to typed exit codes via a central translator in `cmd/exit.go` |
| Analytics `PersistentPreRun` | Inherit existing pattern but forget to track new rollout commands | Wire rollout subcommands into existing `tracker.SendCommandRunEvent`; verify in tests |

---

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Sequential health-check API calls | `start` takes 5-10s even on success | Parallelize with `errgroup`, cap with 5s context deadline | Immediately; agents notice anything >2s |
| Watch polling every 1s | API rate limiting; GraphQL/REST throttling | Default to 5-10s with backoff; honor `Retry-After` | Watching >5 rollouts concurrently, or rate limit of ~60 req/min |
| Status fetches the full rollout history every time | Slow on flags with hundreds of rollouts | Endpoint-specific: status uses "latest" endpoint; list paginates with sensible defaults | At ~50+ historical rollouts per flag |
| List command fetches all rollouts unfiltered | UI lag, agent context blown out | Default `--limit 20`, support pagination | Flags with long rollout histories |
| Watch holds state in memory and never GCs old transition records | Memory growth over multi-hour watches | Bounded ring buffer of last N transitions | Watches running >12h |
| Embedding rollout management in every analytics call | Analytics latency adds to every command | Keep analytics fire-and-forget as already done; don't make rollout commands block on analytics | Always — pattern already established |

---

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Logging the API request body (which includes flag/env context) at debug level by default | Token-adjacent data in shell history / CI logs | Logs at debug level only with explicit `--debug` flag; redact `Authorization` always |
| Including the LD API URL with embedded token in error messages | Token exfiltration via error reporting tools | Strip query params and Authorization headers from any error message |
| Reusing the user's broad-scope personal token for agent-driven rollouts | Compromised agent has full LD account access | Document service tokens as the recommended approach for CI / agents; surface scope requirements in `--help` |
| `--debug-raw` print of API response body where the API returns user PII | PII leak in CI logs | Document that `--debug-raw` may include any field the API returns; redact known PII fields if observed |
| Storing watch state on disk to survive Ctrl-C | State file in plaintext could include sensitive context | Don't persist watch state; rely on rollout ID + re-fetch |
| Trusting upstream API redirect headers | DNS rebinding / token leak to attacker host | Reject 3xx responses with `Location` outside the configured LD base URL |

---

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Using terminology that differs from the LD UI ("ramp" vs. "stage") | Operators can't correlate CLI output to the UI per `REQ-UX-01` | Mirror UI terminology even when the API uses different field names; document mismatches |
| Long `start` command line with 10+ flags | Users copy-paste from chat and miss flags | Support `--config-file path/to/rollout.yaml`; print effective config in confirmation output |
| Status output omits the UI permalink | Operators lose 2 minutes finding it in the UI | Every status / start / stop response includes a `web_url` field (JSON) or "View in LaunchDarkly: ..." line (human) |
| `--watch` UI changes every second (clearing the screen, sparklines) | Output is unparseable in CI / by agents; flicker | Append-only line output by default; `--tty-ui` opt-in for fancy display only when TTY detected |
| Errors don't say what to do next | User reads "validation_failed" and is stuck | Every error message includes a "next step" line: "Try `ldcli ... status --rollout-id ...` to check current state" |
| `list` defaults to all rollouts including a year of history | Wall of text; agent context blown out | Default `--limit 20 --status active,recent`; agent must opt-in to history |
| Help text describes flags without showing examples | Agents have to guess valid combinations | `--help` includes a `Examples:` section with 2-3 invocations covering common cases |
| `--watch` and `status` have inconsistent output schemas | Agents parse one and break on the other | Share the JSON schema; `--watch` is a stream of `status`-shaped objects with an event type wrapper |

---

## "Looks Done But Isn't" Checklist

- [ ] **`start` command:** Often missing — health-check failure detail in JSON output; verify the `health_checks` array is populated on both success and `--skip-health-checks` paths.
- [ ] **`start` command:** Often missing — rollout ID in stdout machine-readable form; verify `ldcli ... start --output json | jq -r .rollout_id` works.
- [ ] **`watch` command:** Often missing — diff-based transition detection; verify a rollout that transitions `running → regressed → rolled_back` between two polls still surfaces the regression event.
- [ ] **`watch` command:** Often missing — clean exit on Ctrl-C; verify SIGINT prints the last-known state and exits with a non-error code.
- [ ] **`watch` command:** Often missing — handling of unknown status values; verify an injected unknown status causes exit 70 with structured error, not a hang.
- [ ] **`status` command:** Often missing — `web_url` field linking to the UI; verify both human and JSON outputs include it.
- [ ] **`stop` command:** Often missing — pre-flight check that rollout is in a stoppable state; verify stopping an already-terminal rollout returns exit 5, not 0 or 1.
- [ ] **`dismiss-regression` command:** Often missing — post-dismiss state in response; verify the agent can confirm the dismiss actually moved the rollout state forward.
- [ ] **Exit codes:** Often missing — distinct codes for "regression detected" (10) vs. "watch timeout" (75); verify both don't collapse to 1.
- [ ] **JSON output:** Often missing — schema documentation; verify `docs/json-schema/` (or similar) has a schema file for every command's JSON output.
- [ ] **JSON output:** Often missing — TTY detection; verify ANSI codes don't appear in JSON output even when run on an interactive terminal.
- [ ] **Errors:** Often missing — `error.code` field that's stable across releases; verify the same logical error produces the same `code` even after the API changes its message.
- [ ] **`API-PAPERCUTS.md`:** Often missing — removal criteria per entry; verify each active workaround has a documented signal for when to remove it.
- [ ] **`API-PAPERCUTS.md`:** Often missing — code cross-references; verify each `// PAPERCUT:` code comment has a matching anchor in the doc.
- [ ] **Analytics:** Often missing — tracking events for new rollout commands; verify `PersistentPreRun` is wired and an analytics event fires per command.
- [ ] **Help text:** Often missing — exit code reference; verify `ldcli flags rollouts-beta --help` documents the exit code contract.
- [ ] **`--skip-health-checks`:** Often missing — audit log of which checks were skipped; verify the success output names them.
- [ ] **Pre-flight checks:** Often missing — flag-on check; verify starting a rollout on an off flag fails with a distinct error code.

---

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| API field renamed, CLI parsing returns zero values | LOW | Update DTO layer field; add golden fixture; release patch. With the DTO discipline, this is one file. |
| Exit codes shipped without classification | MEDIUM | Audit all error paths; add typed errors; map to codes; document. Behavior changes will be observable to agents (good — they want it). |
| `--watch` hangs on unknown state | MEDIUM | Add explicit fatal-on-unknown logic; new test case; release patch. Document the new state in `API-PAPERCUTS.md`. |
| Workaround calcified into permanent code, no one remembers why | MEDIUM | Cross-reference all `// PAPERCUT:` comments with the doc; for orphans, write a "why does this exist?" test and ask the API team. |
| JSON schema changed without doc update | HIGH | Existing agent integrations may be broken in the wild. Diff old vs. new output; document the change; pin schema versions per CLI release; consider an `--output-schema-version` flag for transitional support. |
| Token scope insufficient discovered at runtime by every user | MEDIUM | Add startup pre-flight that checks token capabilities; document required scopes; ship a clearer error message; backport. |
| Watch missed a regression in production | HIGH | Add diff-based detection if missing; add a test case from the actual missed transition; consider a `--include-history` mode that fetches the rollout's event log to backfill. |
| First-consumer assumption baked into multiple files | HIGH | Audit for the assumption; build the abstraction that should have been there from the start; refactor; document as a learning in `API-PAPERCUTS.md`. |

---

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Hidden coupling to API response shapes | Phase 1 (Spike) | Golden fixtures exist; contract test fails on schema drift |
| Misinterpreting API error responses | Phase 2 (Start) | Error taxonomy in code; every observed API error code mapped |
| Building UX around fields that get renamed | Phase 2 (Start) | CLI flag names differ from API field names; translation layer exists |
| Workarounds vs. intentional decisions lost | Phase 1 (Spike) | `API-PAPERCUTS.md` format established with required fields |
| `--watch` never terminates / misses transitions | Phase 4 (Status/Watch) | Diff-based transition detection in code; test for inter-poll transitions |
| Multi-day watch is wrong UX | Phase 4 (Status/Watch) | `--watch-until next-event` default; documentation in `--help` |
| Output that looks machine-readable but is fragile | Phase 0 (Foundations) | Separate `output/human.go` and `output/json.go`; ANSI-on-pipe test |
| Exit codes conflate failure modes | Phase 0 (Foundations) | Exit code contract documented; central `cmd/exit.go` translator |
| Silent fallbacks and mutations | Phase 2 (Start), Phase 3 (Stop/Dismiss) | No auto-correction; mutations echo effective parameters |
| Authentication scope creep | Phase 2 (Start) | Required scope documented in `--help`; 403 errors are specific |
| Race conditions with UI changes | Phase 2 (Start), Phase 3 (Stop/Dismiss) | All mutations target rollout-ID, not just flag; pre-mutation state check |
| Metric/randomization-unit mismatch | Phase 2 (Start) | Pre-flight checks include unit compatibility and recent event volume |
| Health checks slow / blocking | Phase 2 (Start) | Parallelized with `errgroup`; 5s timeout; `start` perf budget tracked |
| Dismiss semantics on terminal rollout | Phase 3 (Stop/Dismiss) | `dismiss-regression` pre-reads state; exits 5 if not actionable |
| Start on incompatible flag state | Phase 2 (Start) | Pre-flight flag state check; distinct exit codes for off / conflicting rules / invalid variation |
| First-consumer assumptions baked in | Phase 1 (Spike), every phase transition | Periodic audit; papercut entry for each assumption surfaced |

---

## Sources

- [gh CLI issue #7401 — handle "no checks" races in `pr checks --watch`](https://github.com/cli/cli/issues/7401)
- [gh CLI issue #463 — `gh pr wait` enhancement request](https://github.com/cli/cli/issues/463)
- [gh CLI manual — `gh pr checks`](https://cli.github.com/manual/gh_pr_checks)
- [Building a CLI That Works for Humans and Machines — openstatus.dev](https://www.openstatus.dev/blog/building-cli-for-human-and-agents)
- [Writing CLI Tools That AI Agents Actually Want to Use — dev.to](https://dev.to/uenyioha/writing-cli-tools-that-ai-agents-actually-want-to-use-39no)
- [Keep the Terminal Relevant: Patterns for AI Agent Driven CLIs — InfoQ](https://www.infoq.com/articles/ai-agent-cli/)
- [We rewrote the Algolia CLI for AI agents — Algolia Engineering Blog](https://www.algolia.com/blog/engineering/we-rewrote-the-algolia-cli-for-ai-agents)
- [Designing CLI Tools for AI Agents: Lessons from Building Memori](https://archit15singh.github.io/posts/2026-02-28-designing-cli-tools-for-ai-agents/)
- [Automated Contract Testing: How to Detect API Drift Before It Reaches Production](https://medium.com/@instatunnel/automated-contract-testing-how-to-detect-api-drift-before-it-reaches-production-6c2a77baa2a3)
- [Schemas Can Be Contracts | Introducing Drift — PactFlow](https://pactflow.io/blog/schemas-can-be-contracts/)
- [Idempotency Is Easy Until the Second Request Is Different — The Coders Blog](https://thecodersblog.com/idempotency-in-distributed-systems-2026/)
- [Handling Race Conditions in Multi-Agent Orchestration — TechAIApp](https://www.techaiapp.com/tech/handling-race-conditions-in-multi-agent-orchestration/)
- ldcli `.planning/codebase/CONCERNS.md` — tech debt and fragile-area context (in-repo)
- ldcli `.planning/PROJECT.md` — milestone requirements (in-repo)

---
*Pitfalls research for: AI-agent-friendly CLI for automated feature flag rollouts (unstable upstream API)*
*Researched: 2026-05-11*
