# Stack Research — Agent-Friendly CLI for Automated Rollouts

**Domain:** AI-agent-friendly CLI extension to an existing Go/Cobra CLI, managing long-running async operations against an unstable upstream API.
**Researched:** 2026-05-11
**Confidence:** MEDIUM-HIGH (most library/version claims verified against official docs; some pattern guidance is best-practice synthesis from primary sources)

> **Scope note:** This is a brownfield milestone. The core stack (Go 1.23, Cobra v1.9.1, Viper v1.21.0, testify v1.11.1, `go.uber.org/mock` v0.5.2, `golang.org/x/term` v0.33.0, `github.com/charmbracelet/*`, etc.) is already established in `.planning/codebase/STACK.md`. This document **only** addresses **net-new ingredients** required to build a `rollouts-beta` command surface that is safe for AI agents to drive.
>
> **Bias toward reuse.** Every recommendation below was checked against what ldcli already has. Where existing infrastructure suffices (e.g. `golang.org/x/term` for TTY detection, the `internal/output` package for JSON/plaintext switching), we recommend reusing rather than introducing a new dependency.

---

## Recommended Stack (Net-New for This Milestone)

### Core Additions

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/hashicorp/go-retryablehttp` | v0.7.7 (latest stable, May 2025) | HTTP retries with exponential backoff for transient failures against `automated-releases` API | Thin wrapper over `net/http`, supports request-body rewinding (critical for POST/PATCH retries), context cancellation, custom retry policies. Used by Terraform, Vault. Lower-friction than introducing a generic retry loop on every call. (HIGH) |
| `github.com/google/uuid` | v1.6.0 *(already in repo)* | Generate client-side `Idempotency-Key` values for `start`/`stop` mutations | Already a dependency (used for analytics client ID); reuse for safe-retry semantics. (HIGH) |
| `briandowns/spinner` *(or status-line written to `cmd.ErrOrStderr()`)* | v1.23.2 (Jan 2025) | Interactive progress indicator for `--watch` and long preflight checks | **Recommend writing a thin internal wrapper** (`internal/output/progress`) that no-ops when `golang.org/x/term.IsTerminal()` is false, mirroring how `gh`'s `iostreams.StartProgressIndicator` behaves. `briandowns/spinner` does *not* auto-detect TTY; you must gate it yourself. (MEDIUM) |

### Patterns / Conventions Adopted (Not Libraries)

| Pattern | Purpose | Why Recommended |
|---------|---------|-----------------|
| **`--output json` produces a stable, schema-versioned object** with a top-level `schemaVersion` field (e.g. `"rollouts.v1beta1"`) | Allows the contract to evolve without silently breaking agent parsers | Mirrors `kubectl`'s `apiVersion`/`kind` envelope and gcloud beta. The `-beta` command suffix and a `schemaVersion` field are belt-and-suspenders: command-tree evolution + output-shape evolution. (HIGH) |
| **NDJSON output for `--watch`** when `--output json` is set | Each polling tick emits one JSON object per line on stdout; agents `jq -c` / line-by-line parse | Standard streaming format for agents (used by Cloud Logging, `docker stats --format`, `gh api --stream`). Avoids the "buffer a giant array forever" anti-pattern. (HIGH) |
| **sysexits.h-aligned exit codes** for `rollouts-beta` commands | Differentiated exit codes let agents make decisions without parsing stderr | See [Exit Code Convention](#exit-code-convention) below. (MEDIUM) |
| **`Idempotency-Key` header on every mutation** (`start`, `stop`, `dismiss`) | Safe to retry on network failure or agent rerun without double-applying | Stripe-style pattern; even if `automated-releases` ignores the header today, sending it future-proofs and documents intent in the papercuts log. (HIGH) |
| **Preflight check pipeline via Cobra `PreRunE`** | Surfaces "you can't do this because <X>" before any state mutation | Use Cobra's `PreRunE`, **not** `RunE`, so a `--dry-run` flag can short-circuit cleanly. Aware of [spf13/cobra#700](https://github.com/spf13/cobra/pull/700) (required-flag validation happens before `PreRunE` only after recent fixes) — verify current Cobra v1.9.1 behavior in test. (MEDIUM) |
| **`signal.NotifyContext`** for `--watch` cancellation | Graceful Ctrl+C cleanup (restore screen buffer, emit final NDJSON record, exit 130) | Modern (Go 1.16+) recommended pattern; superior to manual `signal.Notify` + channel plumbing. (HIGH) |
| **Schema introspection command** — `ldcli flags rollouts-beta schema [--output json]` | Lets agents discover the output shape without parsing docs | Borrowed from the [Google Workspace CLI approach](https://www.theundercurrent.dev/p/rewrite-your-cli-for-agents-or-get) and recent agent-CLI literature. (MEDIUM) |

### Development Tools (Already in repo — no additions needed)

| Tool | Purpose | Notes |
|------|---------|-------|
| `testify` + `go.uber.org/mock` | Test the new `internal/automatedreleases.Client` interface | Follow the existing hand-written `mock_client.go` pattern (see `internal/flags/mock_client.go`); do not introduce mockgen for this domain to match precedent. |
| `cmd.CallCmd` test harness | End-to-end command tests | Already supports `isTerminal` toggle — exercise both TTY (plaintext) and non-TTY (JSON) code paths in tests. |
| `httptest.NewServer` | Test idempotency/retry behavior against a real stub server | Matches existing pattern in `internal/resources/client_test.go`. |

---

## Installation

```bash
# Add to go.mod (single net-new dependency)
go get github.com/hashicorp/go-retryablehttp@v0.7.7

# Optional: add briandowns/spinner only if a wrapped progress indicator is desired.
# If the existing charmbracelet libraries are sufficient for any interactive
# affordance (they already power the quickstart TUI), prefer those.
# go get github.com/briandowns/spinner@v1.23.2

# Then:
make vendor
```

**Note:** `google/uuid` and `golang.org/x/term` are already vendored — do **not** re-add.

---

## Output Format Contract for Agents

The single most important decision for agent-friendliness. Pattern recommendations follow.

### 1. Stable, Versioned JSON Envelope

When `--output json` is set, every command emits a single object on stdout shaped like:

```json
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "RolloutStatus",
  "data": { ... },
  "warnings": [ { "code": "METRIC_HEALTH_DEGRADED", "message": "..." } ]
}
```

**Rationale:**
- `schemaVersion` lets agents pin parsers (`if schemaVersion != "rollouts.v1beta1" { fail or warn }`).
- `kind` discriminates polymorphic results (mirrors Kubernetes resource discrimination).
- `warnings` is a structured channel for non-fatal issues — agents can decide whether to surface or ignore. **Never** emit warnings on stdout as free text; that pollutes the JSON parse.
- Use `vNbetaM` (e.g. `v1beta1`) to align with the `-beta` command suffix and Kubernetes/gcloud conventions. Bump on breaking change.

### 2. NDJSON for `--watch --output json`

Each polling cycle emits **one self-contained JSON object per line** on stdout. Final record sets a terminal flag:

```json
{"schemaVersion":"rollouts.v1beta1","kind":"RolloutTick","ts":"2026-05-11T...","data":{...},"terminal":false}
{"schemaVersion":"rollouts.v1beta1","kind":"RolloutTick","ts":"2026-05-11T...","data":{...},"terminal":false}
{"schemaVersion":"rollouts.v1beta1","kind":"RolloutFinal","ts":"2026-05-11T...","data":{...},"terminal":true,"outcome":"completed"}
```

Agents can stream-parse with `for line in stdout: json.loads(line)` and stop when `terminal=true`. Diff-detection between ticks (only emit when meaningful state changes) reduces token cost — that is the *actionable events* contract called out in REQ-STATUS-03.

### 3. TTY Detection

```go
import "golang.org/x/term"

isTTY := term.IsTerminal(int(os.Stdout.Fd()))
```

`golang.org/x/term` v0.33.0 is **already a transitive dep** in ldcli. Use it; **do not add** `mattn/go-isatty` (we don't need its Cygwin-specific quirks). The existing `cmd.CallCmd` test harness already passes a custom `isTerminal` function — wire any new commands through that.

**Defaults:**
- TTY + no `--output` flag → plaintext.
- Non-TTY + no `--output` flag → **default to JSON**. Mirrors `gh`'s implicit behavior and the Speakeasy/Poehnelt recommendations. This is the single biggest agent ergonomics win.

### 4. stdout vs stderr Discipline

| Stream | Contents |
|--------|----------|
| **stdout** | Only the structured output. JSON: a single object (or one-per-line for `--watch`). Plaintext: parseable tabular/key-value lines. |
| **stderr** | All human-facing chrome: spinners, progress, warnings, error explanations, prompts. |

Agents redirect `2>/dev/null` to get a clean parse. This is non-negotiable for agent-friendliness.

---

## Exit Code Convention

Aligns with [BSD `sysexits.h`](https://man7.org/linux/man-pages/man3/sysexits.h.3head.html) and the Speakeasy/CLIG agent guidance.

| Code | Constant (in `internal/errors`) | Meaning |
|------|-------------------------------|---------|
| 0 | — | Success |
| 1 | `ExitGeneric` | Generic failure (Cobra default; avoid; prefer specific codes below) |
| 2 | `ExitUsage` | Bad flags / args (matches `EX_USAGE`=64 logically; Cobra emits 2 by convention) |
| 64 | `ExitPreflightFailed` | Preflight check rejected the action (e.g. metric health failure, REQ-START-04) — agent should diagnose, not retry |
| 65 | `ExitAPIError` | Upstream API returned a 4xx (excluding auth) — agent should not retry blindly |
| 69 | `ExitAPIUnavailable` | Network/5xx after retries exhausted — agent **may** retry later |
| 75 | `ExitTransientFailure` | E.g. rate-limited — agent should back off |
| 77 | `ExitAuthFailed` | Token expired / forbidden — agent must re-auth |
| 130 | (Go stdlib convention) | `--watch` interrupted by SIGINT — clean shutdown, not a failure |

**Anti-pattern:** Returning exit code 1 for everything. Agents cannot distinguish "retryable" from "your config is wrong" without parsing stderr — which defeats agent friendliness.

**Note:** The Cobra default is to emit `1` on any `RunE` error. Override via the existing root command's error formatter (`internal/errors.NewError` + the root `SilenceErrors: true` + an explicit `os.Exit(code)`). Verify how ldcli already handles this in `cmd/root.go` before introducing new exit-code plumbing.

---

## `--watch` Implementation Pattern (Modeled on `gh pr checks --watch`)

Confirmed by reading [cli/cli/pkg/cmd/pr/checks/checks.go](https://github.com/cli/cli/blob/trunk/pkg/cmd/pr/checks/checks.go):

| Aspect | gh approach | Recommendation for ldcli |
|--------|-------------|--------------------------|
| **Polling interval** | Default 10s, `--interval` configurable in seconds | Default **15s** for rollouts (slower-moving than CI), `--interval` flag, **minimum 5s** clamp to protect the API |
| **Terminal redraw** | `iostreams.StartAlternateScreenBuffer()` + `RefreshScreen()` each tick | When TTY: alternate screen buffer + clear+redraw. When not TTY: emit NDJSON line per tick. Reject `--watch --output json` combined with a TTY-only renderer (gh explicitly errors `cannot use --watch with --json`); for ldcli, **allow it**, emit NDJSON to stdout, omit alternate-screen behavior |
| **Signal handling** | Implicit via OS; alternate-screen cleanup via `StopAlternateScreenBuffer()` | Use `signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)`. On cancel: emit a `terminal:true, outcome:"interrupted"` NDJSON record (agents need to know it didn't finish), restore screen, exit 130 |
| **Diff detection** | gh repaints every tick regardless | **Improvement for ldcli:** keep a `lastState` and only repaint/emit when materially changed (REQ-STATUS-03 actionable events). Plain "still running, no change" ticks waste agent tokens |
| **Exit on terminal state** | `gh run watch` loops `for run.Status != Completed` | Same: exit on `completed`, `failed`, `stopped`, `regression_detected` (if `--fail-on-regression`) |

### Reference: `gh pr checks` watch loop (paraphrased)

```go
if opts.Watch {
    opts.IO.StartAlternateScreenBuffer()
    defer opts.IO.StopAlternateScreenBuffer()
}
for {
    checks, err := populateStatusChecks(...)
    if err != nil { return err }
    if opts.Watch { opts.IO.RefreshScreen() }
    printSummary(checks)
    if allDone(checks) { break }
    select {
    case <-ctx.Done(): return ctx.Err()
    case <-time.After(opts.Interval):
    }
}
```

### Bubbletea — explicitly **NOT** recommended for `--watch`

ldcli already vendors `charmbracelet/bubbletea` v1.3.6 for the interactive quickstart. **Do not** reuse it for `--watch`:

- Bubbletea is heavyweight (full event loop, Elm-style update/view), inappropriate for a status pane that just polls and redraws.
- The `gh` approach (alternate screen + simple redraw) is what users expect from `--watch`.
- Bubbletea's TTY assumptions complicate NDJSON-to-stdout in agent mode.

**Anti-pattern:** Reaching for Bubbletea because it's there. Wrong tool.

---

## Idempotency & Retry Strategy

### Idempotency Keys

On every mutating command (`start`, `stop`, `dismiss`), generate a UUIDv4 and send it as a request header (e.g. `Idempotency-Key` or, if the upstream prefers, `X-LD-Idempotency-Key`):

```go
import "github.com/google/uuid"

req.Header.Set("Idempotency-Key", uuid.NewString())
```

**Even if `automated-releases` does not honor the header today**, sending it is a no-op cost and:
1. Future-proofs against eventual upstream support.
2. Generates a server-side log entry (`Idempotency-Key: <uuid>`) that's invaluable when investigating papercuts.
3. Documents intent. Add a `--idempotency-key <uuid>` flag override so agents can deterministically retry with the same key.

**Document in `.planning/API-PAPERCUTS.md`** whether the API actually deduplicates. This is precisely the kind of first-consumer feedback this milestone is meant to produce.

### Retry Policy (`go-retryablehttp`)

```go
client := retryablehttp.NewClient()
client.RetryMax = 4
client.RetryWaitMin = 500 * time.Millisecond
client.RetryWaitMax = 8 * time.Second
client.CheckRetry = retryablehttp.DefaultRetryPolicy // retries 5xx, network errors, NOT 4xx
client.Logger = nil  // ldcli uses stdlib log; pass a no-op or wrap
```

- Default policy retries network errors + 5xx (except 501) with exponential backoff + jitter.
- 4xx is **never** retried — that's a client error, agents must see it immediately.
- Supports request-body rewinding for retried POST/PATCH (critical for `start`/`stop`).
- Wire `context.Context` from the Cobra command so SIGINT cancels in-flight retries.

**Anti-pattern:** `cenkalti/backoff` alone. It's a backoff primitive, not an HTTP client wrapper — you'd reimplement body rewinding, 5xx detection, and Retry-After handling yourself. `go-retryablehttp` is the higher-leverage choice for an HTTP API client.

**Anti-pattern:** No retry at all. The `automated-releases` API is unstable; transient 502/503/504s during the beta period are likely. Without retry, agents see flaky failures and either back off pessimistically (wasted time) or retry naively without an idempotency key (risk of duplicate rollouts).

---

## Preflight / Health-Check Pattern (REQ-START-04)

```go
// cmd/flags/rollouts/start.go
cmd := &cobra.Command{
    Use: "start",
    PreRunE: func(cmd *cobra.Command, args []string) error {
        return runPreflight(cmd, client, PreflightOpts{
            SkipHealthChecks: skipHealth,
            DryRun:           dryRun,
            Interactive:      term.IsTerminal(int(os.Stdin.Fd())),
        })
    },
    RunE: runStart(client),
}
```

`runPreflight` should:
1. Validate metric health via the LD API (REQ-START-04).
2. On failure in **non-interactive** mode (no TTY OR `--output json`): emit a structured `kind: PreflightFailure` JSON object on stdout, return `errors.NewError("preflight failed")`, exit code **64** (`ExitPreflightFailed`).
3. On failure in **interactive** mode without `--skip-health-checks`: prompt to continue. Use the existing charmbracelet/bubbles confirm dialog pattern from `internal/quickstart/` if a fancy prompt is desired; a stdlib `bufio.Scanner` is also fine.
4. On `--skip-health-checks`: emit a `warnings[]` entry on the eventual success output. Do not silently ignore.
5. On `--dry-run`: render what would happen and exit 0. Critical for agent introspection. Suggested by [poehnelt.com](https://justin.poehnelt.com/posts/rewrite-your-cli-for-ai-agents/).

**Anti-pattern:** Doing preflight inside `RunE` and after some side effects have already happened. Preflight must be a pure read; failure must mean "nothing was changed."

**Anti-pattern:** Returning a free-text error to stderr in JSON-output mode. Agents won't see it.

---

## Beta-Command Gating Strategy

The `-beta` suffix on `rollouts-beta` aligns with industry convention:

| Tool | Convention | Stability signal |
|------|------------|------------------|
| **gcloud** | `gcloud beta <cmd>`, `gcloud alpha <cmd>` separate command groups | Components must be installed separately; output prints `(BETA)` banner |
| **kubectl** | API resource versions: `v1alpha1`, `v1beta1`, `v1`. Beta = 9 months / 3 minor releases before removal | Kubernetes deprecation policy is contractual |
| **gh** | Mostly GA; experimental features hidden behind `gh-` extensions | Less formal |
| **stripe-cli** | Versioned API surface via `--api-version` flag | Output is stable per API version |

### Recommendation for ldcli

1. **Command suffix `-beta`** (chosen): `ldcli flags rollouts-beta start`. Clear, discoverable, allows breaking changes.
2. **First-line stderr banner** on every `rollouts-beta` command in TTY mode:
   ```
   ⚠ rollouts-beta is unstable; the command surface and output schema may change.
     Pin to ldcli version X.Y.Z for production use.
   ```
   Suppress in non-TTY / `--output json` mode (agents don't need decoration).
3. **`schemaVersion: "rollouts.v1beta1"`** on every JSON output, separate from the command-name signal. Both must change together when breaking changes happen.
4. **No separate component install** (unlike gcloud). ldcli is single-binary; the beta surface ships with every release. A future GA migration aliases `rollouts` → `rollouts-beta` then deprecates the suffix.

**Anti-pattern:** Burying instability in a docs paragraph nobody reads. The command name is the contract.

---

## Spinners / Progress UX

| Decision | Recommendation |
|----------|----------------|
| Library | `briandowns/spinner` **v1.23.2** if any interactive spinner is desired. Otherwise: skip, and emit one-line status updates to stderr. |
| TTY gating | `briandowns/spinner` does **not** auto-suppress on non-TTY. **You must** gate it: `if term.IsTerminal(int(os.Stderr.Fd())) { spinner.Start() }` |
| Output target | **stderr only**. Never stdout. (`spinner.WithWriter(os.Stderr)`). Otherwise pipes to `jq` choke on spinner characters. |
| When to use | Long preflight checks (>2s) and the brief gap between `start` API call and first poll response. **Not** inside `--watch`; the watch loop's own redraw is the progress signal. |

### Recommended wrapper sketch

```go
// internal/output/progress/progress.go
package progress

import (
    "io"
    "os"
    "time"
    "github.com/briandowns/spinner"
    "golang.org/x/term"
)

type Indicator interface {
    Start()
    Stop()
    UpdateMessage(string)
}

func New(w io.Writer, message string) Indicator {
    f, ok := w.(*os.File)
    if !ok || !term.IsTerminal(int(f.Fd())) {
        return noopIndicator{}
    }
    s := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(w))
    s.Suffix = " " + message
    return realIndicator{s: s}
}
```

This gives one call site for every command and guarantees agent-mode silence.

**Anti-pattern:** `fmt.Println("Working...")` repeatedly. Pollutes stdout and the agent's transcript.

**Anti-pattern:** Spinner running for 30 minutes during a `--watch`. Use a real status pane (alternate screen buffer) for long-running visibility, not a spinner.

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `hashicorp/go-retryablehttp` | `cenkalti/backoff/v5` | If you need backoff for non-HTTP work (e.g. polling a local file). For HTTP, retryablehttp is strictly better. |
| `hashicorp/go-retryablehttp` | `avast/retry-go` | If you need generic retry semantics beyond HTTP. Less ecosystem traction than retryablehttp. |
| `golang.org/x/term.IsTerminal` (already vendored) | `mattn/go-isatty` | Only if Cygwin/MSYS2 detection on Windows is needed. ldcli's existing `cmd.CallCmd` harness already uses an injected `isTerminal` func — keep that pattern. |
| `briandowns/spinner` | `schollz/progressbar` | Use `progressbar` if you need a **determinate** progress bar (known total). Spinners are for indeterminate operations like the rollout state machine, which is the right primitive here. |
| Custom `--watch` loop (per `gh`) | `charmbracelet/bubbletea` | Use Bubbletea if you need rich, interactive multi-pane TUI (like the quickstart). Watch is single-pane status — Bubbletea is overkill and complicates non-TTY/agent output. |
| NDJSON for `--watch --output json` | Full JSON array buffered until completion | Use full-array only if the operation completes in <30s. Rollouts run for hours/days — NDJSON is mandatory. |
| `signal.NotifyContext` | Manual `signal.Notify` + channel select | Modern stdlib. Manual approach is fine but more boilerplate; the context-based form integrates cleanly with `http.NewRequestWithContext`. |
| Hand-written `MockClient` | `go.uber.org/mock` / `mockgen` | The codebase uses BOTH patterns. For the new `automatedreleases.Client`, follow the hand-written pattern in `internal/flags/mock_client.go` to match closely-related domains. Use mockgen only if the interface gets large/complex. |

---

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **A second JSON output mode (e.g. `--output json-pretty`)** distinct from `--output json` | Two contracts = two parsers for agents | Single `--output json` mode emitting compact JSON. Humans can pipe to `jq .` themselves. |
| **Exit code 1 for every failure** | Defeats agent decision-making; agents can't distinguish "auth failed → re-auth" from "preflight failed → diagnose" without parsing stderr | sysexits-aligned exit codes (see [Exit Code Convention](#exit-code-convention)) |
| **Free-text errors on stderr when `--output json` is set** | Agents parse stdout; stderr is opaque. JSON-mode errors must also be structured (e.g. `{"error":{"code":"PREFLIGHT_FAILED","message":"..."}}`) on stdout, with exit code | Always emit a structured error object on stdout in JSON mode; mirror the success envelope shape |
| **`fmt.Println(spinner)` or any TTY chrome on stdout** | Corrupts pipes to `jq`, breaks agent parse | All chrome (spinners, banners, progress) → `cmd.ErrOrStderr()` |
| **Indefinite client-side retries** | Hides upstream API instability that the papercuts log is supposed to surface | Bounded retry (max 4), structured exit code on exhaustion, log to papercuts |
| **Polling intervals <5s** | Hammers the unstable API; gives no useful UX | Default 15s for `--watch`, clamp `--interval` minimum to 5s |
| **Bubbletea for `--watch`** | Heavyweight TUI framework; over-engineered for a status pane; complicates non-TTY/NDJSON mode | Alternate screen buffer + simple redraw, modeled on `gh pr checks --watch` |
| **`mattn/go-isatty`** | Adds a new dependency for capability already covered by `golang.org/x/term` | `golang.org/x/term.IsTerminal` (already vendored) |
| **A new logging framework** for the rollouts surface | Codebase uses stdlib `log` minimally; introducing zap/zerolog/slog **for one feature** creates inconsistency | Stick with stdlib; route diagnostics to `cmd.ErrOrStderr()`. If structured logs are eventually wanted, that's a separate codebase-wide decision. |
| **`time.Tick` for `--watch`** | Leaks goroutines on cancellation, doesn't compose with context | `time.NewTimer` reset in a loop, or `<-time.After()` inside a `select` with `ctx.Done()` |

---

## Stack Patterns by Variant

**If the operator is interactive (TTY + no `--output` flag):**
- Plaintext output, banner on `rollouts-beta`, spinner during preflight, alternate screen buffer + simple redraw during `--watch`.
- Confirmation prompt on metric health failure unless `--skip-health-checks`.

**If the caller is an AI agent or CI (no TTY OR `--output json`):**
- Default to JSON output, no banner, no spinner, NDJSON for `--watch`.
- No prompts — fail fast with `ExitPreflightFailed` (64) on metric health failure.
- Schema-versioned envelope on every output.
- Structured warnings; never log to stdout.

**If the caller invokes `--dry-run`:**
- Run all preflight checks.
- Emit a `kind: DryRunPlan` JSON object describing what would happen.
- Make **zero** mutating API calls.
- Exit 0 (success). Failed preflight in dry-run still exits 64.

**If `--idempotency-key` is passed explicitly:**
- Use it instead of generating a fresh UUID.
- Lets an agent retry the *exact* same operation safely after a network glitch.

---

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `hashicorp/go-retryablehttp@v0.7.7` | Go 1.13+ (ldcli is on 1.23 ✓) | Stable, low-churn API |
| `briandowns/spinner@v1.23.2` | Go 1.18+ (ldcli is on 1.23 ✓) | Released Jan 2025; no breaking changes since v1.20 |
| `google/uuid@v1.6.0` *(already in repo)* | All current Go versions | No action needed |
| `golang.org/x/term@v0.33.0` *(already transitive)* | All current Go versions | Promote to direct dep if used outside existing call sites |
| `signal.NotifyContext` (stdlib) | Go 1.16+ (ldcli is on 1.23 ✓) | No action needed |

---

## Sources

### HIGH confidence (Context7 / official docs)
- [briandowns/spinner GitHub](https://github.com/briandowns/spinner) — v1.23.2 release, TTY-handling behavior, stderr-writer guidance
- [cli/cli — pkg/cmd/pr/checks/checks.go](https://github.com/cli/cli/blob/trunk/pkg/cmd/pr/checks/checks.go) — `--watch` loop, alternate screen buffer, 10s default interval
- [cli/cli — pkg/cmd/run/watch/watch.go](https://github.com/cli/cli/blob/trunk/pkg/cmd/run/watch/watch.go) — buffer-then-flush refresh pattern, exit-on-terminal-state
- [cli/cli — pkg/iostreams/iostreams.go](https://github.com/cli/cli/blob/trunk/pkg/iostreams/iostreams.go) — TTY detection, alternate screen buffer, conditional spinner
- [cli/cli PR #5681 — alternate screen buffer](https://github.com/cli/cli/pull/5681) — Windows-specific rationale
- [hashicorp/go-retryablehttp](https://github.com/hashicorp/go-retryablehttp) — current API, request-body rewinding, default retry policy
- [Stripe — Idempotent requests](https://docs.stripe.com/api/idempotent_requests) — UUID generation, 24h key TTL, `Idempotent-Replayed` response header
- [Designing robust and predictable APIs with idempotency (Stripe blog)](https://stripe.com/blog/idempotency) — design rationale
- [Kubernetes Deprecation Policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/) — v1alpha/v1beta/v1 contract, 9-month beta deprecation window
- [Google Cloud SDK — Managing components](https://cloud.google.com/sdk/docs/components) — alpha/beta as installable components
- [sysexits.h man page](https://man7.org/linux/man-pages/man3/sysexits.h.3head.html) — exit code 64–78 conventions
- [Command Line Interface Guidelines (clig.dev)](https://clig.dev/) — stdout-for-data, stderr-for-chrome contract
- [signal.NotifyContext — henvic.dev](https://henvic.dev/posts/signal-notify-context/) — modern Go signal handling pattern

### MEDIUM confidence (verified web sources, expert synthesis)
- [Speakeasy — Making your CLI agent-friendly](https://www.speakeasy.com/blog/engineering-agent-friendly-cli) — explicit opt-in modes, structured output, exit-code semantics
- [Justin Poehnelt — Rewrite your CLI for AI agents](https://justin.poehnelt.com/posts/rewrite-your-cli-for-ai-agents/) — `--json`, NDJSON pagination, `--dry-run`, schema introspection
- [Mei Park — Rewrite Your CLI for Agents (theundercurrent.dev)](https://www.theundercurrent.dev/p/rewrite-your-cli-for-agents-or-get) — runtime schema introspection pattern
- [NDJSON spec](https://github.com/ndjson/ndjson-spec) — streaming JSON-per-line conventions
- [BubbleTea v2 release notes / discussion](https://github.com/charmbracelet/bubbletea) — confirms Bubbletea positioning as full TUI framework, not status-pane primitive

### LOWER confidence (best-practice inference)
- 15-second default `--watch` interval for rollouts (slower-than-CI cadence) — extrapolated from `gh pr checks` 10s default and rollout latency characteristics; **validate during implementation**
- Exit code 64 for `ExitPreflightFailed` specifically — sysexits ranges are conventional but not standardized for "precondition violated"; **document the chosen mapping** in user-facing docs

### Existing codebase references (read during research)
- `/Users/alex/code/launchdarkly/ldcli/.planning/PROJECT.md`
- `/Users/alex/code/launchdarkly/ldcli/.planning/codebase/STACK.md` — base stack already in place
- `/Users/alex/code/launchdarkly/ldcli/.planning/codebase/CONVENTIONS.md` — DI patterns, error handling, output patterns
- `/Users/alex/code/launchdarkly/ldcli/.planning/codebase/TESTING.md` — `cmd.CallCmd` harness, mock patterns

---

*Stack research for: AI-agent-friendly CLI extension to ldcli, async/long-running rollout operations*
*Researched: 2026-05-11*
