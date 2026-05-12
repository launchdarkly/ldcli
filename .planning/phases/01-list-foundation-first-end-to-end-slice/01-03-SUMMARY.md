---
phase: 01-list-foundation-first-end-to-end-slice
plan: 03
subsystem: cli
tags: [rollouts, list, cobra-flags, plaintext-table, papercuts, banner-suppression, sort, saturation-warning]

# Dependency graph
requires:
  - phase: 1
    plan: 01
    provides: "rollouts package skeleton + cmd/flags/rollouts subtree + JSON envelope + banner gating"
  - phase: 1
    plan: 02
    provides: "real HTTP path in List/Get + status mapping + error.code taxonomy + raw DTO converters + PAPERCUT source anchors"
provides:
  - "AllFlag / DetailedFlag / LimitFlag constants in cmd/cliflags (with descriptions)"
  - "Extended initListFlags registering --environment, --limit (default 20), --all, --detailed on `flags rollouts-beta list`"
  - "runE wires full ListOpts from Viper, applies defensive deterministic sort, decorates envelope meta.warnings with saturation hint when len(items) == requested limit"
  - "5-column plaintext table (ID, KIND, ENVIRONMENT, STATE, STARTED) + multi-line --detailed records (Target var, Original var, Raw status, Stage progress)"
  - "Production-path deterministic sort in internal/rollouts/client.go.List (CreatedAt DESC, ID ASC)"
  - "cmd.CallCmdWithStderr test helper (additive — existing CallCmd signature unchanged)"
  - "FORCE_TTY env-var honored in shouldPrintBetaBanner to make banner-on-TTY tests deterministic"
  - "14 integration tests covering plaintext/JSON/--json/--detailed/--environment/--limit/default-20/--all/error-envelope/--state-rejected/--idempotency-key-rejected/saturation-warning/sort-order"
  - "3 banner-suppression tests: TTY+plaintext prints; --output json suppresses; non-TTY suppresses"
  - ".planning/API-PAPERCUTS.md seeded with PC-001..PC-016 (DOC-01)"
affects: [04-stop-and-dismiss]

# Tech tracking
tech-stack:
  added: []  # no new dependencies
  patterns:
    - "Defensive sort at the command-layer boundary mirrors the production-path sort in internal/rollouts/client.go — so the CLI contract holds for any Client implementation (including test doubles)"
    - "Saturation warning lives at the envelope-construction site (cmd/flags/rollouts/list.go) rather than inside internal/rollouts/, keeping the internal package free of UI/envelope concerns"
    - "5-col plaintext via text/tabwriter directly — internal/output/table.go assumes flat map-typed `resource` rows, which is the wrong shape for the typed Rollout struct"
    - "Banner gating: FORCE_TTY env var (already used at the root command) drives banner-on-TTY tests inside go test, where stderr is normally not a TTY"
    - "Cross-cutting constraint enforced: persistable user-level settings (AllFlagsHelp map) excludes per-command flags like --all/--detailed/--limit, which only make sense inside a single invocation"

key-files:
  created:
    - cmd/flags/rollouts/list_test.go
    - cmd/flags/rollouts/rollouts_test.go
    - .planning/API-PAPERCUTS.md
  modified:
    - cmd/cliflags/flags.go      (3 new flag constants + descriptions; AllFlagsHelp UNCHANGED so config testdata stays valid)
    - cmd/cmdtest.go             (additive CallCmdWithStderr; existing CallCmd untouched)
    - cmd/flags/rollouts/flags.go    (extended initListFlags: 4 new flags registered)
    - cmd/flags/rollouts/list.go     (runE reads new flags via Viper, builds full ListOpts, applies defensive sort, decorates meta.warnings on saturation, dispatches plaintext detailed vs default; help text documents sort order + PC-003)
    - cmd/flags/rollouts/plaintext.go (Plan 01 placeholder replaced with the real 5-col table + --detailed multi-record renderer using text/tabwriter)
    - cmd/flags/rollouts/rollouts.go (shouldPrintBetaBanner honors FORCE_TTY env var so banner-on-TTY tests work inside go test)
    - internal/rollouts/client.go    (sort.Slice added to List after raw.toRolloutList())

key-decisions:
  - "Defensive sort at BOTH the production client AND the command-layer boundary. Spec said sort lives in internal/rollouts/client.go and the integration test should prove it via MockClient passthrough; but MockClient is dumb-pass-through (bypasses real client), so without a command-layer sort the integration test would always fail. Applying sort at the command layer makes the CLI contract independent of which Client is in use and the test meaningful."
  - "AllFlagsHelp map in cmd/cliflags/flags.go intentionally does NOT include the three new flags. Those are per-command (not persistable as user config) — including them would require updating cmd/config/testdata/help.golden and would suggest to users that `ldcli config --set all=true` is meaningful. The plan acceptance criteria did not require this map to be extended; the constants and descriptions are the deliverable."
  - "Plaintext renderer uses text/tabwriter directly. internal/output/table.go has TableOutput, but it operates on the `resource` map type (interface{}-valued), which would require flattening typed Rollout fields into a map and losing static type safety. Direct tabwriter at the renderer keeps the typed path clean."
  - "FORCE_TTY env var added to shouldPrintBetaBanner. Inside `go test`, os.Stderr is typically a pipe (not a TTY), so term.IsTerminal returns false and the banner-on-TTY test would never fire without an override. FORCE_TTY is the existing root-command pattern; reusing it keeps test scaffolding minimal."

patterns-established:
  - "Integration test pattern for rollouts verbs: `cmd.CallCmd(t, APIClients{RolloutsClient: mockClient}, analytics.NoopClientFn{}.Tracker(), args)` — mirrors `cmd/flags/toggle_test.go` exactly"
  - "Banner-test pattern: `cmd.CallCmdWithStderr` returns (stdout, stderr, err); tests assert on stderr substring for the banner, on stdout for the envelope"
  - "Saturation warning shape: env.Meta.Warnings is a []string; the exact text is `\"List returned exactly %d items; results may be truncated upstream (see API-PAPERCUTS.md PC-003)\"` — agents grep on `PC-003` or `truncated`"
  - "Sort comparator pattern: CreatedAt DESC with ID ASC as deterministic tiebreaker; same code at both layers"

requirements-completed:
  - LIST-01
  - LIST-03
  - DOC-01
  - AGENT-01
  - AGENT-02
  - AGENT-05
  - FOUND-02
  - FOUND-07

# Metrics
duration: ~11m
completed: 2026-05-12
---

# Phase 1 Plan 3: Flag Surface, Plaintext Rendering, Papercuts Doc Summary

**`./ldcli flags rollouts-beta list --flag X --project Y` is feature-complete for Phase 1: --environment/--limit/--all/--detailed wired with Viper; 5-col aligned table + --detailed multi-line records; deterministic sort with help-text contract; saturation warning on meta.warnings when the list is likely truncated upstream; `.planning/API-PAPERCUTS.md` seeded with all 16 cataloged papercuts cross-referenced to source anchors.**

## Performance

- **Duration:** ~11 minutes
- **Started:** 2026-05-12T22:08:43Z
- **Completed:** 2026-05-12T22:19:11Z
- **Tasks:** 2 / 2
- **Files created:** 3 (2 test files + the papercuts doc)
- **Files modified:** 7 (cliflags, cmdtest, list/flags/plaintext/rollouts.go in cmd/flags/rollouts, client.go in internal/rollouts)

## Accomplishments

- Wired the complete Phase 1 CLI surface for `list`: `--environment` (optional filter), `--limit` (int, default 20), `--all` (bool overriding limit), `--detailed` (plaintext-only switch). Default 5-column aligned table; `--detailed` produces multi-line records with every locked field (D-06). JSON output stays the full envelope regardless of `--detailed` (D-07 satisfied via runtime dispatch).
- Locked the AGENT-05 deterministic sort: rollouts emerge from `list` in `(CreatedAt DESC, ID ASC)` order. Help text documents the contract explicitly. Sort is applied at BOTH the production client (`internal/rollouts/client.go`) and the command-layer boundary (`cmd/flags/rollouts/list.go`) so the CLI contract holds for any Client implementation.
- Decorated `meta.warnings` with a saturation hint when `len(items) == requested limit` and `--all` was not set. The warning text references PC-003 directly so agents (or humans) can grep across the codebase + the papercuts doc to understand the root cause.
- Seeded `.planning/API-PAPERCUTS.md` with PC-001..PC-016, each with the full structured fields (Title / Discovered / API behavior / CLI workaround / What we'd prefer / Status / Removal criteria). Verified every `// PAPERCUT: PC-NNN` source-code anchor planted by Plans 01/02 cross-references a real doc entry (PC-002, PC-003, PC-004, PC-005, PC-011, PC-013, PC-014 all confirmed).
- Added `cmd.CallCmdWithStderr` as a sibling to the existing `cmd.CallCmd` — required because the banner writes to stderr and existing tests cannot observe it. The 30+ existing `CallCmd` callsites are untouched (additive helper, not signature change).
- Made the banner gating testable inside `go test` by honoring `FORCE_TTY` (mirrors the existing root-command pattern). All four (TTY × output-kind) combinations now have explicit test coverage: TTY+plaintext prints; TTY+json suppresses; non-TTY+plaintext suppresses; non-TTY+json suppresses (covered by combination of three tests).

## Task Commits

1. **Task 1: List flag surface + sort + saturation + plaintext + 17 tests** — `a8a1486` (feat)
   - 9 files changed, +810 / -38
   - 14 list integration sub-tests + 3 banner-suppression sub-tests; all PASS
   - Defensive sort at both production and command layer; saturation warning via env.Meta.Warnings; FORCE_TTY banner gating
2. **Task 2: API-PAPERCUTS.md seeded with PC-001..PC-016** — `29faabc` (docs)
   - 1 file created, 201 insertions
   - Active count: 16, Resolved count: 0; every source-code `// PAPERCUT: PC-NNN` anchor cross-references a doc entry

**Plan metadata commit:** the SUMMARY.md commit follows below.

## Files Created/Modified

**New files:**

- `cmd/flags/rollouts/list_test.go` — 14 sub-tests via `cmd.CallCmd` + `rollouts.MockClient`. Covers plaintext, JSON envelope, `--json` shorthand, `--detailed` (plaintext-only effect), `--environment`/`--limit`/default-20/`--all`, error-envelope shape, unknown-flag rejection for `--state` (D-04) and `--idempotency-key` (deferred to Phase 2), saturation warning, and deterministic sort order.
- `cmd/flags/rollouts/rollouts_test.go` — 3 banner-suppression sub-tests using `cmd.CallCmdWithStderr`. Verifies FORCE_TTY + plaintext prints the banner; `--output json` always suppresses; non-TTY stderr always suppresses.
- `.planning/API-PAPERCUTS.md` — 16 papercut entries with structured fields, Active Index table, source-code cross-references. Phase 1 milestone reference embedded; entries PC-001/PC-006..PC-010/PC-012/PC-015/PC-016 are forward-looking (Phase 2-4); PC-002/PC-003/PC-004/PC-005/PC-011/PC-013/PC-014 already have source anchors.

**Modified:**

- `cmd/cliflags/flags.go` — Added `AllFlag = "all"`, `DetailedFlag = "detailed"`, `LimitFlag = "limit"` in the const block (alphabetized). Added matching `AllFlagDescription` / `DetailedFlagDescription` / `LimitFlagDescription` constants. AllFlagsHelp map UNCHANGED (intentional — see Decisions).
- `cmd/cmdtest.go` — Added `func CallCmdWithStderr(t, clients, trackerFn, args) (stdoutBytes, stderrBytes []byte, err error)`. Existing `CallCmd` signature untouched.
- `cmd/flags/rollouts/flags.go` — Extended `initListFlags` with four new flags via `cmd.Flags().String/Int/Bool` + `viper.BindPFlag`. Required vs optional preserved (only `--flag` and `--project` are required).
- `cmd/flags/rollouts/list.go` — runE reads new flags via Viper, builds full `ListOpts{Environment, Limit, All}`, applies defensive `sortRolloutsByRecency`, decorates `env.Meta.Warnings` when `!All && Limit > 0 && len(items) >= Limit`, dispatches `RenderRolloutListPlaintext(list, detailed)`. Help text updated with reverse-chronological sort + PC-003 pagination caveat (AGENT-05).
- `cmd/flags/rollouts/plaintext.go` — Replaced Plan 01 placeholder with `renderTable` (5-col tabwriter) and `renderDetailed` (multi-line records with every D-06 field including Target var, Original var, Raw status, Stage progress). Empty fields render as em-dash (U+2014).
- `cmd/flags/rollouts/rollouts.go` — `shouldPrintBetaBanner` now honors `FORCE_TTY` and `LD_FORCE_TTY` env vars (matches root-command pattern). Banner-on-TTY tests inside `go test` flip FORCE_TTY=1 to exercise the print path.
- `internal/rollouts/client.go` — Added `sort.Slice` over `list.Items` after `raw.toRolloutList()` to lock the (CreatedAt DESC, ID ASC) order in the production HTTP path. Code comment explains the layering boundary (saturation warning lives at the envelope site, not here).

## A2 Investigation Result (per plan output spec)

**A2: Does the upstream API response include `environmentKey` directly?**

**Outcome: Plan 02 already confirmed via hand-crafted fixtures.** Plan 02's `internal/rollouts/testdata/*.json` fixtures include both `environmentId` (UUID) and `environmentKey` (slug); the converter (`toRollout`) passes both through with `omitempty`. Plan 03 did not need to investigate further — the field shape is locked. No PC-017 was added; if a live staging test eventually shows `environmentKey` is missing from production responses, that finding would surface a new papercut at that time (and the converter's `_links.self.href` parsing fallback would land in the same PR).

## internal/output/table.go Reusability (per plan output spec)

**Outcome: NOT reusable for the typed Rollout struct.** `internal/output/table.go` exposes `TableOutput(items []resource, cols []ColumnDef)` where `resource` is `map[string]interface{}` (a flat untyped map). The typed `rollouts.Rollout` struct would need to be flattened into a map (losing static typing) and the nested `StatusBlock` would need extra logic to drill into `Status.Kind`. The cleaner path is `text/tabwriter` directly at the renderer, which keeps the typed path clean and matches the layering Plan 01 already established (envelope marshaling bypasses `output.CmdOutput` for the same reason).

**Action for Phase 2-3:** The `status` and `watch` verbs should mirror this pattern — use `text/tabwriter` directly inside the per-verb plaintext renderer rather than threading through `output.TableOutput`. If a generalized typed renderer is ever needed, factor a small `internal/rollouts/plaintext` helper out of `cmd/flags/rollouts/plaintext.go`; do not extend `internal/output/table.go`.

## Deferred Decision: `--limit 0` Semantics (per plan output spec)

**Outcome: `--limit 0` is silently treated as the default.** The runE reads `viper.GetInt(LimitFlag)` which returns `0` only when explicitly set to `0`; the saturation check uses `opts.Limit > 0` as a guard, so `--limit 0` simply disables the saturation warning. The client's URL builder (`internal/rollouts/client.go`) treats `Limit <= 0` as "send `limit=20` to the upstream" (existing Plan 02 behavior). Neither `0` nor negative inputs trigger a CLI error — they fall through to defaults.

**Action for Phase 2 ergonomics work:** if operators explicitly use `--limit 0` to mean "unlimited" by analogy with curl or jq, that intent is currently NOT honored — `--all` is the canonical way to request the full history. If user reports surface this confusion, reject `--limit < 1` with an error in a future plan, OR alias `--limit 0` to `--all`. Logged here as a deferred-decision marker.

## Phase 1 Requirements Coverage Confirmation (per plan output spec)

All 18 Phase 1 requirement IDs are reflected in source or tests:

| ID | Status | Source / test |
|---|---|---|
| FOUND-01 | Plan 01 | `internal/rollouts/` package skeleton |
| FOUND-02 | Plan 01 (banner) + Plan 03 (test) | `cmd/flags/rollouts/rollouts.go` + `rollouts_test.go` |
| FOUND-03 | Plan 01 | `internal/rollouts/models.go` (Envelope) |
| FOUND-04 | Plan 02 | `internal/rollouts/errors.go` `mapAPIError` |
| FOUND-05 | Plan 02 | `internal/rollouts/client.go` retryablehttp config |
| FOUND-06 | Plan 01 | `internal/rollouts/client.go` Logger=nil |
| FOUND-07 | Plan 01 + 03 | `shouldPrintBetaBanner` (banner gating) |
| FOUND-08 | Plan 01 + 02 | ErrCode* enum + mapAPIError taxonomy |
| DOC-01 | Plan 03 | `.planning/API-PAPERCUTS.md` (16 entries) |
| LIST-01 | Plan 01 | List in Client interface + envelope shape |
| LIST-02 | Plan 02 | Real HTTP path + 18 round-trip tests |
| LIST-03 | Plan 03 | `--environment`/`--limit`/`--all`/`--detailed` wired |
| AGENT-01 | Plan 01 | `--output json` inherited from root persistent flag |
| AGENT-02 | Plan 01 + 02 | `error.code` taxonomy + exit-1 contract |
| AGENT-03 | Plan 02 | Idempotency-Key helper exists (Phase 2 exercises) |
| AGENT-04 | Plan 02 | int64-millis → time.Time + duration-string converters |
| AGENT-05 | Plan 03 | Sort + help-text + integration test for order |

All 17 IDs above are accounted for. The 18th ID per ROADMAP §"Phase 1 Requirements" — checking the explicit count again: FOUND-01..08 (8), DOC-01 (1), LIST-01..03 (3), AGENT-01..05 (5) = 17 total. The plan listed 18 in its output spec, which appears to be a transcription artifact (the 17-ID count is correct per the requirements file). All are addressed.

## New Papercuts Discovered During Plan 03 (per plan output spec)

**None.** The 16 cataloged papercuts (PC-001..PC-016) cover the full surface area exercised by Plan 03. No new upstream API behavior surfaced during this plan that wasn't already cataloged in `.planning/research/ARCHITECTURE.md`. If a live staging validation surfaces additional papercuts (most likely PC-017 around `environmentKey` if it's actually missing from the production response — see A2), they'll be appended in a future plan.

## Decisions Made

- **Defensive sort at both layers.** The plan specified sort lives in `internal/rollouts/client.go` only and the integration test would prove it via MockClient passthrough. But `MockClient` is a dumb pass-through that doesn't go through the real client — so without a command-layer sort, the integration test would always fail (the mock returns items in input order, and the command emits them as-is). I applied sort at the command boundary as well so (a) the integration test is meaningful, (b) the CLI contract is independent of which Client implementation is in use, and (c) the production code path still has the sort for HTTP-driven invocations. Code comments on both sort sites cross-reference each other.
- **`AllFlagsHelp` map unchanged.** Including `all` / `detailed` / `limit` in the map (alongside existing entries like `environment`, `flag`, `output`) would have:
  - made `cmd/config/testdata/help.golden` go stale (test failure in `cmd/config`)
  - suggested to users that `ldcli config --set all=true` is meaningful, when those flags are per-command and only apply to `flags rollouts-beta list`
  - The plan acceptance criteria only required the flag-name constants and descriptions, not extending the map. Decision documented in code comment on `AllFlagsHelp`.
- **`text/tabwriter` directly in plaintext.go, not `internal/output/TableOutput`.** Same rationale as Plan 01's choice to bypass `output.CmdOutput` for the envelope: the existing helper operates on flat untyped maps; the rollouts typed struct is the wrong shape. Hard-rolling a small renderer is cheaper than retrofitting the helper.
- **`FORCE_TTY` env var added to banner gating.** Mirrors the existing root-command pattern. Tests inside `go test` cannot otherwise force the banner-on-TTY path because stderr is normally a pipe in the test runner. Adding `FORCE_TTY` to the banner predicate (in addition to the existing `term.IsTerminal(os.Stderr.Fd())` check) is the smallest change that makes the banner-on-TTY test possible.

## Deviations from Plan

**Auto-fixed Issues:**

**1. [Rule 1 — Bug] `cmd/config` test regression from `AllFlagsHelp` extension**

- **Found during:** Task 1, after first GREEN run, `go test ./...` revealed `cmd/config TestNoFlag` failure.
- **Issue:** Adding `AllFlag` / `DetailedFlag` / `LimitFlag` to `cliflags.AllFlagsHelp()` extended the user-facing config-settings help that `ldcli config` lists. The golden file `cmd/config/testdata/help.golden` does not include these new entries (and shouldn't — they're not user-configurable persistent settings, they're per-command flags).
- **Fix:** Reverted the `AllFlagsHelp` map extension. The three new constants + descriptions remain in `cliflags/flags.go` (they're used by the rollouts command's flag registration). Added a doc comment on `AllFlagsHelp` documenting why per-command flags are intentionally excluded.
- **Files modified:** `cmd/cliflags/flags.go` (revert).
- **Verification:** `go test ./cmd/config/...` passes; `go test ./cmd/flags/rollouts/...` continues to pass.
- **Committed in:** `a8a1486` (rolled into Task 1).

This is a Rule 1 (bug) auto-fix because the plan's `<acceptance_criteria>` did not require AllFlagsHelp extension — the plan only required the constants + descriptions. Extending the map was an over-implementation that broke a sibling package. Scope: limited to reverting the map; no other code changed.

**2. [Rule 3 — Blocking] Sort-order test could not pass via MockClient alone**

- **Found during:** Task 1 RED phase analysis (before writing implementation).
- **Issue:** The plan specified the sort lives in `internal/rollouts/client.go` and the integration test would prove it via MockClient passthrough. But MockClient is a dumb pass-through (it returns whatever `.Return(...)` is configured, unmodified). If sort lives only in the real client, the integration test would always fail because the mock bypasses real-client logic.
- **Fix:** Added a small defensive `sortRolloutsByRecency` helper at the command-layer boundary that applies the same comparator. Documented the design choice in code comments at both sort sites. The CLI contract (AGENT-05) holds for any Client implementation now.
- **Files modified:** `cmd/flags/rollouts/list.go` (added sortRolloutsByRecency call + helper).
- **Verification:** `TestListSortOrder` passes with the mock returning deliberately unsorted items.
- **Committed in:** `a8a1486` (rolled into Task 1).

This is a Rule 3 (blocking) auto-fix because the integration test could not pass otherwise — the plan's own test spec required end-to-end sort verification with a MockClient. Scope: limited to a small helper at the command layer; the production client sort is unchanged.

**3. [Rule 3 — Blocking] Banner test couldn't fire without FORCE_TTY support**

- **Found during:** Task 1 test design.
- **Issue:** `shouldPrintBetaBanner` checks `term.IsTerminal(os.Stderr.Fd())` directly. In `go test`, stderr is a pipe (not a TTY), so the banner is suppressed unconditionally — making `TestBetaBanner/prints_beta_banner_on_TTY` impossible to satisfy.
- **Fix:** Extended `shouldPrintBetaBanner` to honor `FORCE_TTY` and `LD_FORCE_TTY` env vars (mirroring the existing root-command pattern). Tests set `FORCE_TTY=1` via `t.Setenv` to flip the predicate.
- **Files modified:** `cmd/flags/rollouts/rollouts.go` (added env-var check).
- **Verification:** All three banner-suppression tests pass; production behavior unchanged when env vars are unset.
- **Committed in:** `a8a1486` (rolled into Task 1).

This is a Rule 3 (blocking) auto-fix because the banner-on-TTY test could not exist otherwise. The override mirrors an existing convention (FORCE_TTY at the root command), so it doesn't introduce a new pattern.

**No other deviations.** Both tasks executed substantively as planned; all acceptance-criteria grep checks pass; the regression suite is green across all 27 ldcli packages.

## Known Stubs (intentional — replaced by Phase 2 / Phase 3 / Phase 4)

Phase 1's `list` is feature-complete; no list-specific stubs remain. The following Phase 1-level stubs (already documented in Plan 01 and Plan 02 SUMMARYs) are still in place but are out of scope for Plan 03:

| Location | Stub | Replacement |
|---|---|---|
| `internal/rollouts/idempotency.go` (SetIdempotencyKey) | Helper exists, no call site exercises it | Phase 2 (Start instruction) |
| `internal/rollouts/instructions.go` | StartInstruction / StopInstruction / DismissRegressionInstruction have only Kind field | Phase 2 / Phase 4 |
| `cmd/flags/rollouts/` | No `start` / `status` / `stop` / `dismiss` verbs yet | Phase 2-4 |

## Threat Surface Scan

No new threat surface beyond what Plan 01 / Plan 02 / Plan 03 `<threat_model>` documented. Mitigations verified:

- **T-03-01 (URL injection via --environment):** `internal/rollouts/client.go` builds the filter via `url.Values{}.Set()` and `q.Encode()`; no string concatenation of user-supplied `--environment` into the URL. Verified: `grep -E 'path \+ "\?.*\+ opts\.Environment' internal/rollouts/client.go` returns zero. ✓
- **T-03-02 (Plaintext injection):** The renderer only formats typed fields (strings the API produced, ints, time.Time). User-supplied CLI flags (`--flag`, `--project`, `--environment`) control the API call, not the rendered rows; they're never echoed into output text. ✓
- **T-03-03 (meta.warnings info disclosure):** The saturation warning format string interpolates only an int (`opts.Limit`). No user-supplied string flows into the warning text. ✓
- **T-03-04 (papercut doc tampering):** Doc is internal to the repo, not loaded at runtime. ✓
- **T-03-05 (--all DoS):** `--all` requests `limit=1000` (existing Plan 02 behavior); the retryablehttp envelope caps total wall time at ~16s. Saturation warning informs operators when the cap was hit. ✓
- **T-03-06 (analytics repudiation):** Plan 03 introduces no new analytics events. Existing `flags-rollouts-beta` action-tagged event continues to honor `analytics-opt-out`. ✓
- **T-03-07 (test harness spoofing):** `CallCmdWithStderr` is additive; the existing `CallCmd` signature is untouched (`git log --oneline cmd/cmdtest.go` confirms the diff is a pure addition). ✓

No new threat flags to file.

## Verification

**make test** — all 27 packages PASS:

```text
ok  	github.com/launchdarkly/ldcli/cmd	1.549s
ok  	github.com/launchdarkly/ldcli/cmd/analytics	0.310s
ok  	github.com/launchdarkly/ldcli/cmd/config	0.634s
... 24 more ...
ok  	github.com/launchdarkly/ldcli/internal/rollouts	1.821s
ok  	github.com/launchdarkly/ldcli/internal/sdks	1.940s
```

**make build** — succeeds:

```text
$ make build
go build -o ldcli
```

**Help text invariants:**

```text
$ ./ldcli flags rollouts-beta list --help | grep -E 'reverse-chronological|deterministic tiebreaker'
Rollouts are returned in reverse-chronological order by createdAt timestamp,
with rollout ID as the deterministic tiebreaker.

$ ./ldcli flags rollouts-beta list --help | grep -E '\-\-(environment|limit|all|detailed)'
      --all                  Return all rollouts (ignores --limit; subject
      --detailed             Plaintext only: include variations, ended-at,
      --environment string   Filter rollouts by environment key (optional)
      --limit int            Maximum number of rollouts to return (default
```

**Unknown-flag rejection (D-04 + Phase 2 deferral):**

```text
$ ./ldcli flags rollouts-beta list --state foo --flag x --project y --access-token t
unknown flag: --state
exit 1

$ ./ldcli flags rollouts-beta list --idempotency-key abc --flag x --project y --access-token t
unknown flag: --idempotency-key
exit 1
```

**Network-error CLI smoke test:**

```text
$ ./ldcli flags rollouts-beta list --flag x --project y --access-token t --output json --base-uri https://invalid.example.test
{
  "schemaVersion": "rollouts.v1beta1",
  "kind": "Error",
  "error": {
    "code": "network_error",
    "message": "Network error: ...",
    "nextAction": "Check connectivity and retry; if persistent, check firewall/proxy settings"
  }
}
exit 1
```

**Papercuts doc integrity:**

```text
$ test -f .planning/API-PAPERCUTS.md && echo OK; OK
$ grep -cE '^### PC-' .planning/API-PAPERCUTS.md; 16
$ grep -c 'Active count: 16' .planning/API-PAPERCUTS.md; 1
$ grep -c 'Resolved count: 0' .planning/API-PAPERCUTS.md; 1
$ for PC in $(grep -rohE 'PC-[0-9]+' internal/rollouts/ cmd/flags/rollouts/ | sort -u); do grep -q "^### $PC " .planning/API-PAPERCUTS.md && echo "$PC OK"; done
PC-002 OK
PC-003 OK
PC-004 OK
PC-005 OK
PC-011 OK
PC-013 OK
PC-014 OK
```

**Source-grep acceptance criteria:**

```text
grep -c 'AllFlag\s*=\s*"all"' cmd/cliflags/flags.go        → 1
grep -c 'DetailedFlag\s*=\s*"detailed"' cmd/cliflags/flags.go  → 1
grep -c 'LimitFlag\s*=\s*"limit"' cmd/cliflags/flags.go    → 1
grep -c 'cliflags\.AllFlag' cmd/flags/rollouts/flags.go     → 2 (registration + binding)
grep -c 'cliflags\.LimitFlag' cmd/flags/rollouts/flags.go   → 2
grep -c 'cliflags\.DetailedFlag' cmd/flags/rollouts/flags.go → 2
grep -c 'cliflags\.EnvironmentFlag' cmd/flags/rollouts/flags.go → 2
grep -c 'sort\.Slice' internal/rollouts/client.go            → 1
grep -c 'reverse-chronological order' cmd/flags/rollouts/list.go → 1
grep -c 'PC-003' cmd/flags/rollouts/list.go                 → 4
grep -c 'func CallCmdWithStderr' cmd/cmdtest.go             → 1
```

## Self-Check: PASSED

Files created exist:

- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a938344d568df6c5a/cmd/flags/rollouts/list_test.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a938344d568df6c5a/cmd/flags/rollouts/rollouts_test.go` — FOUND
- `/Users/alex/code/launchdarkly/ldcli/.claude/worktrees/agent-a938344d568df6c5a/.planning/API-PAPERCUTS.md` — FOUND

Commits exist in git log:

- `a8a1486` (Task 1) — FOUND
- `29faabc` (Task 2) — FOUND
