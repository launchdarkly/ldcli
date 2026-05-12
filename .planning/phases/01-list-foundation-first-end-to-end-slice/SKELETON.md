---
phase: 01-list-foundation-first-end-to-end-slice
created: 2026-05-12
type: walking-skeleton
status: planned
---

# Walking Skeleton: ldcli flags rollouts-beta

> The thinnest end-to-end deliverable for the rollouts-beta surface. Phase 1's Plan 01
> ships this skeleton; subsequent plans incrementally swap stubs for real implementations
> without changing the architectural shape.

## What the Skeleton Proves

Before any feature work, this skeleton proves the **entire pipeline is wired end-to-end**:

```
user CLI invocation
  → cmd/flags/rollouts/list.go (Cobra RunE)
    → internal/rollouts/Client.List (interface)
      → RolloutsClient.List (concrete impl, real HTTP later; stub for now)
        → JSON envelope construction (schemaVersion=rollouts.v1beta1, kind=RolloutList)
          → stdout (via json.MarshalIndent or plaintext renderer)
beta banner emission → stderr (TTY-gated)
analytics event → backend
```

If `make build && ./ldcli flags rollouts-beta list --flag foo --project bar` returns
a syntactically valid envelope and exits 0 (with a hardcoded stub response), the skeleton
is done. Reality (real API call, retry layer, 13-state mapping, papercuts annotations,
full ergonomics) gets layered on in Plans 02 and 03 without the architectural shape moving.

## Architectural Decisions Locked by the Skeleton

These decisions ship in Plan 01 and are not renegotiated by Plans 02–03 or subsequent phases.

| Decision | Locked value | Source |
|---|---|---|
| Package location for typed client | `internal/rollouts/` (sibling to `internal/flags/`) | D-08, PATTERNS.md |
| Command tree location | `cmd/flags/rollouts/` (sub-package under `cmd/flags`) | FOUND-02, RESEARCH.md |
| Command path | `ldcli flags rollouts-beta <verb>` | FOUND-02 |
| HTTP client | dedicated `retryablehttp.Client` owned by `RolloutsClient`; does NOT route through `internal/resources/Client` | RESEARCH.md §Retry Layer |
| Net-new dep | `github.com/hashicorp/go-retryablehttp@v0.7.8` only | RESEARCH.md §Standard Stack |
| Envelope schema version | `"rollouts.v1beta1"` (string constant `SchemaVersionV1Beta1`) | FOUND-03 |
| Envelope shape | `{schemaVersion, kind, data, meta, error}` | FOUND-03, D-07 |
| Status shape (per rollout) | NESTED: `data.status = {status, kind, label}`; top-level `data.kind` = rollout kind (guarded\|progressive) | D-02 + RESEARCH.md A1 (nested) |
| Exit codes | Any error → exit 1 (Cobra default); rich `error.code` enum in envelope | D-01 |
| TTY detection | reuse existing `cmd/root.go:222-227` for `--output` default; new `term.IsTerminal(os.Stderr.Fd())` only for banner | FOUND-07 |
| Beta banner placement | stderr, suppressed when JSON or non-TTY stderr | FOUND-02, RESEARCH.md §Banner |
| Mock pattern | testify-mock returning typed `*RolloutList` / `*Rollout` (not `[]byte`) | PATTERNS.md mock_client.go |
| Idempotency layer | `internal/rollouts/idempotency.go` wired but not exercised; user-facing `--idempotency-key` flag deferred to Phase 2 | D-08, RESEARCH.md §Idempotency |
| Client interface scope | Phase 1: `List` + `Get` only; Phase 2 adds `Start`; Phase 4 adds `Stop`/`DismissRegression` | D-08 |
| Status three-field model | `status` (raw API enum), `kind` (5-bucket: active\|regressed\|reverted\|paused\|completed), `label` (human string) | D-02 |
| Plaintext default | 5-col table (ID, kind, environment, state, started) | D-06 |
| `--detailed` scope | plaintext expansion only; JSON always full per D-07 | D-06, D-07 |
| List default scope | most recent 20, reverse-chronological by `createdAt` DESC, ID ASC tiebreaker | D-05, AGENT-05 |
| `--state` filter | NOT shipped in v1 | D-04 |

## Skeleton Components (Plan 01 deliverable)

| Component | What ships in Plan 01 | What's stubbed | What Plan 02/03 replaces |
|---|---|---|---|
| `go.mod` / `vendor/` | `go-retryablehttp@v0.7.8` added; vendored | — | (final) |
| `internal/rollouts/client.go` | `Client` interface (`List`+`Get`), `RolloutsClient` struct, `NewClient(version)`, `newRetryableClient()` helper | `List`/`Get` method bodies return a hardcoded `*RolloutList` literal (no HTTP call) | Plan 02 wires real HTTP via `retryablehttp` + DTO conversion |
| `internal/rollouts/models.go` | All DTO types (`Rollout`, `Stage`, `Event`, `MetricConfiguration`, `Link`, `RolloutList`, `StatusBlock`, `Envelope`, `EnvelopeError`, `EnvelopeMeta`), `SchemaVersionV1Beta1` const | (nothing stubbed; types are complete) | Plan 02 adds raw-layer types + converter funcs |
| `internal/rollouts/envelope.go` | `NewListEnvelope`, `NewErrorEnvelope` helpers | — | (final) |
| `internal/rollouts/mock_client.go` | testify `MockClient`, `var _ Client = &MockClient{}` | — | (final) |
| `internal/rollouts/client_test.go` | Sanity test: stub returns expected envelope shape | — | Plan 02 replaces with `httptest.NewServer` round-trip tests |
| `cmd/flags/rollouts/rollouts.go` | `NewRolloutsCmd(client, trackerFn)` parent cmd; `PersistentPreRun` with banner + analytics; subcommand registration | — | (final) |
| `cmd/flags/rollouts/list.go` | `NewListCmd(client)`; `runE` reads Viper, calls `client.List`, marshals envelope, writes to stdout | Stub `ListOpts` is empty (no flag wiring beyond `--flag`/`--project`) | Plan 03 adds `--environment`/`--limit`/`--all`/`--detailed` wiring + plaintext rendering |
| `cmd/flags/rollouts/flags.go` | `initListFlags(cmd)` registering `--flag`/`--project` as required | `--environment`, `--limit`, `--all`, `--detailed` NOT yet registered | Plan 03 registers them |
| `cmd/flags/rollouts/plaintext.go` | Minimal `RenderRolloutListPlaintext(list)` — single-line-per-rollout dump (not the 5-col table yet) | Table rendering | Plan 03 replaces with full 5-col table + `--detailed` |
| `cmd/cliflags/flags.go` | (no change in Plan 01) | — | Plan 03 appends `AllFlag`/`DetailedFlag`/`LimitFlag` constants |
| `cmd/root.go` | `APIClients.RolloutsClient`, `rollouts.NewClient(version)` in `Execute()`, `rolloutscmd.NewRolloutsCmd(...)` wired as child of `flags` | — | (final) |
| Beta banner copy | Two lines: `⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change.` followed by an indented second line `  Pin to ldcli vX.Y.Z for production use.` (the `X.Y.Z` placeholder is interpolated from `cmd.Root().Version` at runtime). This is the authoritative banner copy; Plan 01 emits it verbatim from `cmd/flags/rollouts/rollouts.go`. | — | (final) |
| `internal/rollouts/errors.go` | `RolloutError` struct + `ErrCode*` enum constants + `mapAPIError`/`mapTransportError` skeletons returning `ErrCodeUnknownUpstream` for now | Real status-code → error-code mapping | Plan 02 fills in 401/403/404/409/400/429/5xx mapping |
| `internal/rollouts/status_mapping.go` | (not in Plan 01) | All of it | Plan 02 ships full 13-state mapping |
| `internal/rollouts/idempotency.go` | `SetIdempotencyKey(req)` one-liner using `google/uuid` | — | Phase 2 exercises it on mutations |
| `internal/rollouts/instructions.go` | Skeleton struct types (`SemanticPatch`, `StartInstruction`, `StopInstruction`, `DismissRegressionInstruction`) | All field shapes | Phase 2 fleshes Start; Phase 4 fleshes Stop |
| `.planning/API-PAPERCUTS.md` | (not in Plan 01) | All of it | Plan 03 seeds with PC-001..PC-016 |

## How to Verify the Skeleton Works (after Plan 01)

```bash
# 1. Compile
make build

# 2. Run with stub client — returns a syntactically valid envelope on stdout
./ldcli flags rollouts-beta list --flag any-flag --project any-proj --output json
# Expected stdout: {"schemaVersion":"rollouts.v1beta1","kind":"RolloutList","data":{"items":[]},"meta":{...}}
# Expected stderr: (empty in JSON mode — no banner)
# Expected exit: 0

# 3. Run in TTY mode — banner emits to stderr
./ldcli flags rollouts-beta list --flag any-flag --project any-proj
# Expected stderr line 1: "⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change."
# Expected stderr line 2: "  Pin to ldcli vX.Y.Z for production use." (X.Y.Z is the CLI version)
# Expected stdout: plaintext rendering (one line per stub rollout)
# Expected exit: 0

# 4. Run unit tests
make test
# Expected: all green; internal/rollouts/client_test.go passes (stub returns expected shape)

# 5. Confirm wiring
./ldcli flags rollouts-beta --help    # rollouts-beta listed under flags
./ldcli flags --help                  # rollouts-beta appears in subcommands
```

## Subsequent Plans Replace the Stubs

- **Plan 02 — Real HTTP + status mapping + error taxonomy:** swap stub client body for real `retryablehttp` GET against `/internal/projects/{p}/flags/{flagKey}/automated-releases`; full DTO converter (`raw.toRolloutList()`); 13-state mapping in `status_mapping.go`; full `error.code` mapping in `errors.go`; idempotency helper finalized; `httptest.NewServer` round-trip tests including retry behavior + 401/404/5xx paths.
- **Plan 03 — List ergonomics + papercuts doc:** add `--environment`, `--limit`, `--all`, `--detailed` flags + Viper bindings; client-side sort; saturation warning; 5-col plaintext table + `--detailed` expanded form; cmd-level integration tests via `CallCmd` + `MockClient`; seed `.planning/API-PAPERCUTS.md` with all 16 papercuts and annotate Phase 1 workaround sites with `// PAPERCUT: PC-NNN`.

## Skeleton Anti-Patterns to Avoid

- **DO NOT** route the rollouts HTTP through `internal/resources/Client` — it has no retry layer and adding retry there changes behavior for every existing ldcli command.
- **DO NOT** route the envelope through `internal/output/CmdOutput` — that dispatcher operates on flat `resource` maps and would lose the typed envelope shape. Rollouts marshals its own envelope via `json.MarshalIndent`.
- **DO NOT** re-implement TTY detection for `--output`; reuse the existing pattern at `cmd/root.go:222-227`. The banner is the only TTY check Plan 01 adds.
- **DO NOT** add `mattn/go-isatty`; `golang.org/x/term` is already in use.
- **DO NOT** add `automated-releases` paths to `ld-openapi.json` or hand types through the generated code path. Types are hand-rolled.
- **DO NOT** add a numeric exit-code taxonomy. Per D-01, exit codes stay 1 for any error; `error.code` in the envelope carries the distinction.
- **DO NOT** expose `--idempotency-key` as a user-facing flag in Phase 1 (no mutations exist to exercise it; debuts in Phase 2 alongside `start`).

## Validation of Phase 1 Decisions (one-time tasks owned by Plan 01)

The skeleton also lands two architectural validations that downstream plans depend on:

1. **A1 — Nested status envelope shape.** Plan 01 commits the `Rollout.Status = StatusBlock{status, kind, label}` nesting (per RESEARCH.md A1 + D-02). No further negotiation.
2. **A2 — `environmentKey` presence in API response.** Plan 01 attempts a manual `curl` (or `httptest.NewServer` stub matching staging-captured fixture) to confirm whether `environmentKey` is in the API response body or must be parsed from `_links.self.href`. Result is recorded in `internal/rollouts/models.go` as a comment and (if missing) appended to `API-PAPERCUTS.md` in Plan 03 as a new PC-NEW entry.

---

*Walking Skeleton planned: 2026-05-12*
