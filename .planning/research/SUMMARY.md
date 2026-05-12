# Project Research Summary

**Project:** ldcli — Automated Rollouts via CLI (`flags rollouts-beta`)
**Domain:** AI-agent-friendly CLI extension for long-running async operations against an unstable upstream API
**Researched:** 2026-05-11
**Confidence:** MEDIUM-HIGH overall; HIGH for stack patterns and architecture (primary sources); MEDIUM for specific API behaviors (undocumented API, validated from source code)

## Executive Summary

This milestone adds a `ldcli flags rollouts-beta` command surface to an existing mature Go/Cobra CLI. The domain is well-understood (other tools such as `kubectl argo rollouts` and `gh pr checks` provide direct analogs), but the specific upstream API (`automated-releases` in gonfalon) is undocumented, unstable, and ldcli will be its first real consumer. The core implementation pattern is clear: hand-rolled types in `internal/rollouts/`, mutations via the existing semantic-patch flag-patch endpoint (`PATCH /api/v2/flags/...`), and observability via direct REST calls to `/internal/projects/.../automated-releases/`. The structural risk is not "how to build this" but "how to build it so it doesn't break when the API evolves."

The most important cross-cutting decision is the **output contract**: every command must share a versioned JSON envelope (`schemaVersion: "rollouts.v1beta1"`, `kind`, `data`, `meta.availableActions`, `meta.warnings`), a sysexits-aligned exit code set, and clean stdout/stderr separation from day one. This contract is the foundation that both the agent-friendliness story and the `--watch` NDJSON event stream depend on. Getting it right in Phase 1 (Foundations) means all subsequent phases are additive; getting it wrong means every phase carries a migration tax. The `API-PAPERCUTS.md` deliverable is not optional documentation — it is the primary feedback channel from this milestone to the gonfalon team, and the architecture research has already seeded 16 candidate entries.

The two highest-risk architectural facts are: (1) `startAutomatedRelease` returns the updated `FeatureFlag`, not the new `AutomatedRelease`, so every `start` call requires a follow-up GET (Architecture P1 / Pitfall 9); and (2) there is no dedicated pre-flight validation endpoint, so the `--skip-health-checks` bypass (REQ-START-04) must be handled via the `recommended-duration` proxy endpoint as an imperfect signal (Architecture P8). Both should be documented as papercuts immediately and worked around in implementation without waiting for upstream fixes.

## Key Findings

### Recommended Stack

The base stack (Go 1.23, Cobra v1.9.1, Viper, testify, `go.uber.org/mock`, `golang.org/x/term`) is already in place and should be reused without modification. The only net-new dependency warranted is `github.com/hashicorp/go-retryablehttp@v0.7.7` for HTTP retries with exponential backoff and automatic request-body rewinding on POST/PATCH retries — critical for idempotent mutations against an unstable API. `google/uuid` (already vendored) provides client-side `Idempotency-Key` values for all mutations. TTY detection reuses `golang.org/x/term.IsTerminal` (already a transitive dependency) rather than introducing `mattn/go-isatty`. The `--watch` loop follows the `gh pr checks --watch` pattern (alternate screen buffer + simple redraw) and explicitly avoids `charmbracelet/bubbletea`, which is already vendored for the quickstart TUI but is wrong for a simple status pane. NDJSON (one JSON object per line) is the streaming format for `--watch --output json`.

**Core technologies:**
- `github.com/hashicorp/go-retryablehttp@v0.7.7`: HTTP retries with backoff for unstable API — strictly better than `cenkalti/backoff` for HTTP because it handles 5xx detection and body rewinding automatically
- `github.com/google/uuid@v1.6.0` (already vendored): client-side `Idempotency-Key` generation for all mutations
- `golang.org/x/term@v0.33.0` (already transitive): TTY detection gating all interactive affordances
- `signal.NotifyContext` (stdlib, Go 1.16+): clean `--watch` cancellation, exit 130 on SIGINT
- Versioned JSON envelope pattern (`schemaVersion: "rollouts.v1beta1"`): agent output contract independent of API churn

**Key pattern decisions:**
- Non-TTY or `--output json` defaults to JSON output (implicit agent-friendliness, mirrors `gh`)
- All human-facing chrome (spinners, banners, prompts) goes to `cmd.ErrOrStderr()` only, never stdout
- Preflight in `PreRunE`, not `RunE`, so `--dry-run` exits before any mutation
- `--watch` defaults to actionable-event exit, not terminal-state exit, for multi-day rollouts

### Expected Features

No comparable flag-management CLI offers metric pre-flight health checks before rollout creation — that is the differentiator this surface must own for AI-agent safety. The verb-noun shape (`start / stop / list / status / dismiss-regression`) converges with every comparable tool surveyed. `--watch` with NDJSON event streaming and `--until` predicates is the key agent-UX differentiator beyond what `gh pr checks` offers today.

**Must have (table stakes):**
- `rollouts-beta start` with all rollout options as explicit flags — agents need no interactive prompts
- `rollouts-beta list <flag>` with deterministic reverse-chronological ordering
- `rollouts-beta status <flag>` with bucketed top-level state (`running` / `paused` / `succeeded` / `failed` / `regression-detected`) atop raw API enum
- `rollouts-beta status --watch` with NDJSON event stream and bounded `--timeout`
- `rollouts-beta stop <flag> --to-variation <key>` idempotent
- `rollouts-beta dismiss-regression <flag>` idempotent
- `--output json` envelope with `schemaVersion`, `meta.availableActions`, `meta.uiURL`, `meta.warnings`
- Documented exit code contract (see below)
- API papercuts log (`API-PAPERCUTS.md`) maintained throughout

**Should have (differentiators):**
- Default-fail on metric health-check problems; `--skip-health-checks` with audit log of what was skipped
- `--watch --until=<event>` (default `terminal`; also `regression`, `stage-advanced`)
- `start --dry-run` returning a `kind: DryRunPlan` JSON envelope with zero server-side mutations
- Bucketed status field with stable enum + raw API status alongside for debugging
- `meta.availableActions` next-action hints on every rollout-describing response
- Idempotent `stop` and `dismiss-regression` (exit 0 with `alreadyApplied: true` on double-call)
- `--watch --timeout` with exit code 8 distinguishing "timed out, still running" from failure

**Defer (v2+):**
- Zero-arg `start` driven by release policies (not yet GA)
- Auto-generated commands from OpenAPI spec (API must stabilize first)
- `pause`/`resume` verbs (API does not expose them today)
- Cross-environment promotion workflow (compose v1 primitives instead)
- MCP server wrapper around the CLI surface
- Interactive TUI wizard for `start`

**Exit code contract (required in Phase 1):**

| Code | Meaning | Agent action |
|------|---------|-------------|
| 0 | Success (incl. idempotent no-op) | Continue |
| 2 | Usage / bad flags | Fix invocation |
| 3 | Resource not found | Check inputs |
| 4 | Auth / permission denied | Re-auth |
| 5 | Conflict (already running, already stopped) | Check state |
| 7 | Metric health-check failed | Fix instrumentation |
| 8 | Watch timeout (rollout still running) | Re-watch or poll |
| 9 | Regression detected (from watch, with `--exit-on-regression`) | Escalate / decide |
| 70 | Unknown upstream error code | Escalate; CLI may need update |
| 130 | SIGINT during `--watch` | Not a failure |

### Architecture Approach

The architecture is a clean brownfield extension following existing ldcli patterns. Mutations (`start`, `stop`) use the existing public flag semantic-patch endpoint (`PATCH /api/v2/flags/{p}/{flagKey}`) with instruction kinds `startAutomatedRelease` and `stopAutomatedRelease`. Observability (`list`, `status`, `dismiss-regression`) uses direct REST calls to `/internal/projects/.../automated-releases/...` — which accept the same account token as the public API, despite the `/internal/` prefix. Types must be hand-rolled in a new `internal/rollouts/` package because the endpoints are not in ldcli's `ld-openapi.json` and the API is too unstable for code generation. The critical runtime fact is that `start` always requires two round-trips: the patch call returns a `FeatureFlag`, not the new `AutomatedRelease`, so a follow-up `GET .../automated-releases?filter=environmentKey:{ek}&limit=1` is always needed.

**Major components:**
1. `cmd/flags/rollouts/` (new): thin Cobra subcommands (`start`, `stop`, `list`, `status`, `dismiss_regression`) following `cmd/flags/toggle.go` style
2. `internal/rollouts/` (new): typed `Client` interface + `RolloutsClient` impl, `models.go` (hand-rolled `AutomatedRelease` types), `instructions.go` (semantic-patch envelope), `mock_client.go`
3. `internal/output/` (existing, extended): add column registrations for automated-releases; share JSON envelope shape across all rollout commands
4. `cmd/cliflags/flags.go` (existing, extended): new constants for `--release-kind`, `--target-variation`, `--original-variation`, `--randomization-unit`, `--stages`, `--metrics`
5. `.planning/API-PAPERCUTS.md` (new): structured papercut log, 16 seeds from architecture research

**Key data flows:**
- `start`: PATCH flag semantic-patch → follow-up GET list (env-filtered, limit=1) → return `items[0]` as rollout
- `status`: GET single release → parallel GET metric-results per metric → optional GET diagnostics → render
- `dismiss-regression`: PATCH metric-state (returns 204) → re-fetch with backoff until status reflects dismissal → render
- `watch`: GET single release in loop → diff against previous state → emit only new/changed events → exit on terminal or actionable state

### Critical Pitfalls

1. **Output contract designed late** — If JSON envelope shape, exit codes, and stdout/stderr discipline are added per-command rather than defined upfront, every subsequent phase carries a migration tax and agents see an inconsistent schema across commands. Prevention: define the envelope type, exit code map, and separate human/JSON output paths in Phase 1 Foundations before writing any command.

2. **`start` returns no rollout ID without a follow-up GET** — The `startAutomatedRelease` patch response is a `FeatureFlag`, not the new `AutomatedRelease`. Agents that do `ldcli start | jq .id` get nothing. Prevention: always perform the two-step start (patch + re-fetch), surface the rollout ID prominently in JSON output, and document this as API-PAPERCUTS.md P1.

3. **No dedicated pre-flight endpoint for metric health** — The closest proxy is `GET recommended-duration`, which runs much of the same validation but does not return per-metric pass/fail detail. `--skip-health-checks` therefore bypasses something imprecise. Prevention: use `recommended-duration` as the pre-flight signal, surface the specific error message when it fails, document API-PAPERCUTS.md P8, and parallelize multi-metric validation calls with `errgroup` (5s deadline).

4. **`--watch` that misses inter-poll transitions** — Rollout state can transition `running → regression_detected → rolled_back` between two polls. A status-only poller sees only `rolled_back` and never surfaces the regression. Prevention: diff against previous state on every poll; emit every event from `release.events` not seen in the previous poll; treat unknown status enum values as fatal-for-watch (exit 70, log as papercut).

5. **Hidden coupling to unstable API response shapes** — The `automated-releases` API is undocumented and ldcli is the first consumer; field renames happen with no notice. Prevention: thin DTO layer in `internal/rollouts/` separate from the CLI domain model; golden response fixtures in `testdata/`; contract tests that fail on schema drift; `// PAPERCUT: <anchor>` inline comments for every workaround.

## Implications for Roadmap

Based on combined research, five phases emerge. The ordering respects two hard dependencies: the output contract must precede all commands (every command shares it), and `status` (read-only) must precede `watch` (which reuses the status fetcher). `start` is the highest-risk phase because it involves the most API surface, the most pre-flight logic, and the two-step round-trip pattern.

### Phase 1: Foundations

**Rationale:** The output contract, exit code map, `internal/rollouts/` client skeleton, and `API-PAPERCUTS.md` template are shared infrastructure that every subsequent phase builds on. Starting here ensures no per-command rework later. Pitfall research explicitly flags output contract and exit codes as "Phase 0 / Foundations" items.

**Delivers:**
- `internal/rollouts/` package skeleton: `Client` interface, empty `RolloutsClient`, `models.go` (hand-rolled `AutomatedRelease` types from Architecture research), `mock_client.go`, `instructions.go` (semantic-patch envelope)
- JSON output envelope type (`schemaVersion: "rollouts.v1beta1"`, `kind`, `data`, `meta`) shared by all commands
- Exit code contract in `cmd/exit.go` or `internal/errors/` with all codes defined upfront
- `API-PAPERCUTS.md` with the 16 seeds from Architecture research, in structured format (anchor ID, discovered date, workaround, removal criteria)
- `go-retryablehttp` wired into `RolloutsClient` with retry policy (max 4, 500ms–8s backoff, 4xx never retried)
- `Idempotency-Key` header sent on every mutation (document as papercut if API does not honor it)
- Progress indicator wrapper (`internal/output/progress`) that no-ops on non-TTY
- Beta banner on TTY, suppressed on non-TTY / `--output json`

**Avoids:** Pitfall 7 (fragile machine-readable output), Pitfall 8 (conflated exit codes), Pitfall 4 (workarounds vs. intent lost)
**Research flag:** Standard patterns — no additional research needed

### Phase 2: Read-Only Commands (`list` + `status`)

**Rationale:** Read-only commands validate the entire client/auth/output plumbing against the real API without risk of creating or modifying state. They surface papercuts about the response shape (P2–P6, P11–P15) before any mutation logic is written. Architecture research explicitly suggests this ordering.

**Delivers:**
- `rollouts-beta list <flag>` — deterministic reverse-chronological ordering, `--limit`, `--environment` filter
- `rollouts-beta status <flag>` — bucketed top-level state + raw status, stage details, metric configurations (for guarded), `meta.availableActions`, `meta.uiURL`
- Golden response fixtures in `internal/rollouts/testdata/` from staging API calls
- Contract tests validating schema against fixtures
- Diagnostics sub-fetch wired into `status` for "why is data not flowing?" output

**Uses:** `go-retryablehttp`, `internal/rollouts/Client`, JSON envelope from Phase 1
**Avoids:** Pitfall 1 (API coupling without contract tests), Pitfall 5 (status enum without bucketing)
**Research flag:** Validate watch timing/backoff values empirically during this phase against staging

### Phase 3: Start

**Rationale:** `start` is the highest-risk command (semantic-patch mutation + two-step round-trip + pre-flight health checks + most flags). Isolating it to its own phase bounds the blast radius of API surprises. The two-step pattern (patch + re-fetch) must be implemented here, and the `recommended-duration` pre-flight proxy must be exercised against staging. Architecture research flags this as "highest API risk; expect rework."

**Delivers:**
- `rollouts-beta start` with full flag surface: `--release-kind`, `--target-variation`, `--original-variation`, `--randomization-unit`, `--stages`, `--metrics`, `--auto-rollback`, `--extension-duration`, `--rule-id`
- Two-step start implementation: PATCH + re-fetch GET returning rollout ID in JSON output
- Pre-flight via `recommended-duration` endpoint (unless `--skip-health-checks`); parallel metric validation calls with `errgroup`; 5s timeout
- `--skip-health-checks` with audit log of skipped checks in success output
- `start --dry-run` returning `kind: DryRunPlan` with zero mutations
- Flag state pre-flight (flag on? valid variation? no conflicting rollout already running?)
- `--idempotency-key` override for agent deterministic retries
- Papercut P1, P8, P10, P12–P14, P16 documented with CLI workarounds

**Avoids:** Pitfall 3 (CLI flags coupled to API field names — translate in instructions.go), Pitfall 9 (silent fallbacks), Pitfall 10 (auth scope — document required role in --help), Pitfall 11 (race conditions — target rollout ID, not just flag), Pitfall 12 (metric/unit mismatch), Pitfall 13 (slow health checks — parallelize), Pitfall 15 (incompatible flag state)
**Research flag:** Validate pre-flight behavior of `recommended-duration` against staging for both guarded and progressive kinds; validate exact error shapes from `startAutomatedRelease` for unmapped codes

### Phase 4: Mutations (`stop` + `dismiss-regression`)

**Rationale:** Smaller scope than `start`; reuses the semantic-patch helper from Phase 3 and the re-fetch pattern from Phase 2. Both commands require careful idempotency handling and pre-mutation state checks. Architecture research confirms `stop` infers rollout type server-side — caller only needs `finalVariationId`. `dismiss-regression` returns 204 and requires a follow-up re-fetch with backoff (Architecture P7 / Anti-Pattern 3).

**Delivers:**
- `rollouts-beta stop <flag> --to-variation <key>` — pre-reads rollout state; exits 5 if not stoppable; idempotent (`alreadyApplied: true` on double-call)
- `rollouts-beta dismiss-regression <flag>` — pre-reads state; exits 5 if not in `monitoring_regressed`; re-fetches after 204 with backoff until dismissal confirmed; `--json` includes post-dismiss state
- Papercut P7, P9 documented

**Avoids:** Pitfall 14 (dismiss on terminal rollout), Pitfall 11 (race conditions — pre-mutation state reads)
**Research flag:** Standard patterns — validate dismiss backoff timing empirically against staging

### Phase 5: Watch + Polish

**Rationale:** `--watch` reuses the status fetcher from Phase 2 and adds diff-based event detection, NDJSON streaming, TTY alternate-screen buffer, and bounded timeout. Polish locks exit codes, schema documentation, and the API-PAPERCUTS.md deliverable. Architecture research treats watch as a separate phase (R4) because it requires a stable status fetcher underneath it.

**Delivers:**
- `rollouts-beta status --watch [--interval N] [--timeout D] [--until EVENT]`
- Diff-based transition detection (not status-only polling) — emits `stage-advanced`, `regression-detected`, `regression-dismissed`, `action-required`, `terminal` events
- NDJSON streaming when `--output json` (one object per line, `terminal: true` on final record)
- TTY: alternate screen buffer + clear+redraw per tick; non-TTY: NDJSON per tick
- `signal.NotifyContext` for SIGINT: emit `terminal: true, outcome: "interrupted"` then exit 130
- Unknown status values: exit 70 + papercut entry (never hang)
- Watch default of "until next actionable event" (not terminal) for multi-day rollout awareness
- Exit code 8 for `--timeout` expiry while rollout still running
- JSON schema documentation for all commands
- Exit code reference in `rollouts-beta --help`
- `API-PAPERCUTS.md` finalized — all active workarounds have removal criteria and code cross-references
- Analytics `PersistentPreRun` wired for all rollout subcommands

**Avoids:** Pitfall 5 (`--watch` never terminates or misses transitions), Pitfall 6 (multi-day watch is wrong UX), Pitfall 7 (fragile output — ANSI test, RFC3339 timestamps)
**Research flag:** Validate 15s default interval and exponential backoff ceiling against API rate limits

### Phase Ordering Rationale

- **Foundations first** because the output contract and exit code map are shared infrastructure — building them per-command creates inconsistency and rework. Both Pitfalls and Stack research identify this as "Phase 0."
- **Read-only before mutations** because reads validate the entire client/auth/response-parsing pipeline with zero risk. Golden fixtures captured here anchor contract tests for all later phases.
- **`start` before `stop/dismiss`** because `start` introduces the semantic-patch helper, the two-step round-trip pattern, and the pre-flight pipeline — all of which `stop` and `dismiss-regression` reuse.
- **`watch` last** because it requires a stable status fetcher (Phase 2) and is pure CLI UX with no new endpoints — the lowest-risk slot for a non-trivial UX feature.
- This ordering matches Architecture research's suggested build order (R1 through R5) with Foundations extracted as a distinct pre-phase.

### Research Flags

Phases needing additional research or empirical validation during planning/execution:

- **Phase 3 (Start):** Pre-flight behavior of `recommended-duration` against staging for guarded vs. progressive — does it surface per-metric errors or only aggregate? What error shapes does `startAutomatedRelease` return for unmapped validation failures? Validate before building error taxonomy.
- **Phase 5 (Watch):** Validate 15s default interval and backoff ceiling against gonfalon rate limits (~60 req/min/token). Confirm whether `monitoring_regressed` should trigger immediate watch exit or continue polling.

Phases with well-established patterns (skip additional research):

- **Phase 1 (Foundations):** JSON envelope, exit code design, TTY detection, retry policy — all backed by high-confidence sources (gh source, sysexits.h, go-retryablehttp docs, CLIG guidelines).
- **Phase 2 (Read-only):** List/GET patterns are standard; golden fixture + contract test pattern is established.
- **Phase 4 (Stop/Dismiss):** Semantic-patch wrapper and re-fetch pattern established in Phase 3; only the 204 backoff timing needs empirical validation (low risk).

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Library recommendations verified against official docs; patterns confirmed from gh/kubectl source code; spinner library and watch interval timing are MEDIUM |
| Features | HIGH | Table stakes verified against 8+ comparable CLIs; differentiators grounded in multiple agent-CLI industry sources; anti-features all have explicit rationale |
| Architecture | HIGH | Primary sources are gonfalon source files read directly; endpoint shapes, auth model, instruction fields, and validation rules are from the implementation; 16 papercuts cataloged |
| Pitfalls | MEDIUM-HIGH | 16 pitfalls with explicit prevention strategies and phase assignments; some specific API behaviors (idempotency key support, exact dismiss-regression behavior) need staging validation |

**Overall confidence:** MEDIUM-HIGH

### Gaps to Address

- **`Idempotency-Key` header behavior**: Architecture research recommends sending it; whether gonfalon honors it for deduplication is unverified. Document in API-PAPERCUTS.md in Phase 1; validate in Phase 3 spike.
- **`recommended-duration` as pre-flight proxy**: The endpoint runs similar validation to `startAutomatedRelease` but is not a dedicated validation endpoint. The error detail it returns (per-metric vs. aggregate) needs staging validation before building the health-check UX in Phase 3.
- **`waiting` status value**: Architecture research notes this enum value is undocumented (P6). Watch-mode behavior when this is encountered needs a decision before Phase 5. Default to treating as non-terminal until clarified with the API team.
- **`dismiss_regression` eventual consistency window**: Architecture Anti-Pattern 3 recommends 1s/3s backoff re-fetch with ~10s timeout. Exact timing needs empirical measurement in Phase 4.
- **Exit code alignment between STACK.md and FEATURES.md**: STACK.md proposes sysexits-aligned codes (64, 65, 69, 75, 77); FEATURES.md proposes a simpler sequential set (0–9, 70, 75). Reconcile in Phase 1 before any command is written. The sequential set from FEATURES.md is recommended for ergonomics; document the sysexits.h logical equivalents in code comments.

## Sources

### Primary (HIGH confidence — direct source code or official docs)

- `gonfalon/internal/flags/instruction/instruction_start_automated_release.go` — Start instruction fields, validation rules, target resolution logic
- `gonfalon/internal/flags/instruction/instruction_stop_automated_release.go` — Stop instruction, server-side rollout type inference
- `gonfalon/internal/experimentation/releaseguardian/internal/api/api.yaml` — Authoritative OpenAPI for all `/internal/.../automated-releases/` endpoints
- `gonfalon/internal/experimentation/releaseguardian/internal/api/automated_release_transformations.go` — AutomatedRelease response shape, status enum mapping
- `gonfalon/internal/experimentation/releaseguardian/internal/api/internal_patch_measured_rollout_metric_state.go` — Dismiss-regression 204 and "Hack" comment (papercut P7)
- [cli/cli — pkg/cmd/pr/checks/checks.go](https://github.com/cli/cli/blob/trunk/pkg/cmd/pr/checks/checks.go) — `--watch` loop, alternate screen buffer, 10s interval model
- [hashicorp/go-retryablehttp](https://github.com/hashicorp/go-retryablehttp) — retry policy, request-body rewinding, 4xx exclusion
- [sysexits.h man page](https://man7.org/linux/man-pages/man3/sysexits.h.3head.html) — exit code 64–78 conventions
- [Command Line Interface Guidelines (clig.dev)](https://clig.dev/) — stdout/stderr discipline contract
- [kubectl argo rollouts CLI reference](https://argo-rollouts.readthedocs.io/en/stable/generated/kubectl-argo-rollouts/) — verb-noun shape, watch patterns, promote/abort vocabulary
- [Stripe idempotency docs](https://docs.stripe.com/api/idempotent_requests) — Idempotency-Key pattern and 24h TTL

### Secondary (MEDIUM confidence — community sources, expert synthesis)

- [Speakeasy — Making your CLI agent-friendly](https://www.speakeasy.com/blog/engineering-agent-friendly-cli)
- [Justin Poehnelt — Rewrite your CLI for AI agents](https://justin.poehnelt.com/posts/rewrite-your-cli-for-ai-agents/)
- [Mei Park — Rewrite Your CLI for Agents](https://www.theundercurrent.dev/p/rewrite-your-cli-for-agents-or-get)
- [Writing CLI Tools That AI Agents Actually Want to Use](https://dev.to/uenyioha/writing-cli-tools-that-ai-agents-actually-want-to-use-39no)
- [Building a CLI That Works for Humans and Machines](https://www.openstatus.dev/blog/building-cli-for-human-and-agents)
- [Algolia Engineering — rewrote CLI for AI agents](https://www.algolia.com/blog/engineering/we-rewrote-the-algolia-cli-for-ai-agents)
- gh CLI issues [#7401](https://github.com/cli/cli/issues/7401), [#463](https://github.com/cli/cli/issues/463) — watch race conditions and transition detection

### Tertiary (LOW confidence — inference, needs staging validation)

- 15-second default `--watch` interval — extrapolated from `gh pr checks` 10s default and rollout stage latency characteristics; validate against API rate limits in Phase 5
- `recommended-duration` as metric health-check proxy — inferred from validation code path; exact error detail needs staging validation before Phase 3
- `dismiss-regression` eventual consistency backoff timing (1s/3s/10s) — inferred from the "Hack" comment in gonfalon source; needs empirical measurement in Phase 4

---
*Research completed: 2026-05-11*
*Ready for roadmap: yes*
