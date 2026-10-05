# Generated checks: format and rules

## Layout

```
.verify-out/pr-<N>/generated/
  checks.json          manifest
  <id>.sh              one script per check
```

```json
{
  "schema": 1,
  "checks": [
    {
      "id": "events-query-cancellation-overhead",
      "title": "Debug-events query adds no per-row goroutine/allocations under a request context",
      "kind": "discriminating",
      "rationale": "go-sqlite3 1.14.51 stopped spawning a goroutine + channel per row when ctx.Done() != nil; events_db.QueryEvents iterates rows under the HTTP request context.",
      "script": "events-query-cancellation-overhead.sh",
      "severity": "attention",
      "timeout": 300
    }
  ]
}
```

- `id`: kebab-case, unique.
- `kind`: `discriminating` (expected: fails on base, passes on PR) or `guard` (expected: passes on both).
- `severity`: what a regression means, either `attention` (needs human, the default) or `block`.
- `timeout`: seconds (default 600).

## Script contract

The runner runs each script twice: once with `SIDE=base` and `WT=<base worktree>`, once with `SIDE=pr` and `WT=<pr worktree>`. In both runs the working directory is `$WT`, `LD_*` is unset, and the XDG config, state, and data dirs are private. The scripts must:

1. `source "$VERIFY_ROOT/lib/check.sh"`
2. Do their work against `$WT` only. Write scratch files under `$ARTIFACTS`, never into the worktree. If you must touch the worktree, call `restore_tree` before finishing.
3. Finish with exactly one of `pass "summary"`, `fail "summary"`, `warn "summary"`, or `skip "reason"`. If the script exits without one of these (a crash or timeout), the result is recorded as `error`.

Useful helpers: `detail "markdown line"` (shown in the comment), `detail_block file [lines]`, `recommend "action"`, `run cmd…` (echoes the command to the log), `build_ldcli <path>`, `ensure_ui_deps`, `free_port`, `updates_for <ecosystem>`, `$UI_DIR_REL`.

For Go-level behavior, the most reliable pattern is a throwaway test file. Copy it into the package under test, run only that test, then delete it:

```bash
#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
cp "$(dirname "$0")/sqlite_probe_test.go.txt" "$WT/internal/dev_server/db/zz_verify_probe_test.go"
trap 'rm -f "$WT/internal/dev_server/db/zz_verify_probe_test.go"' EXIT
if (cd "$WT" && run go test -count=1 -run '^TestVerifyProbe$' ./internal/dev_server/db/); then
  pass "cached statement sees the new column"
fi
fail "cached statement returns stale columns"
```

(Name helper files `*.txt` so `go` tooling never picks them up from the generated dir.)

For CLI behavior, build with `build_ldcli "$ARTIFACTS/ldcli"` and assert on output. For the dev server, start it as `binary-smoke.sh` does: a free port, `--access-token verify-smoke-placeholder`, and `XDG_STATE_HOME` under `$ARTIFACTS`.

## Rules

1. **It must fail on the old version before it counts.** The runner enforces this. A discriminating check counts only with outcome `proven` (base fails, PR passes). If it passes on both, it is reported as "does not count". If you expected it to discriminate, the check probably doesn't reach the changed code; rework it.
2. **Behavior, not versions.** `sqlite_version() >= 3.50` or "package.json says 2.3.2" proves nothing about ldcli. Exercise code paths that ldcli uses.
3. **Reachability first.** Every check must name the ldcli code path it covers in `rationale`. If the changed code is unreachable from ldcli, write a guard for the ldcli behavior closest to it (or none), and state the unreachability in `impact.json`.
4. **Deterministic and quiet.** No sleeps-as-synchronization, no reliance on wall-clock time, no network unless the summary says so. Two runs must give the same result.
5. **Don't weaken the baseline.** Generated checks add to the baseline; they never replace or skip baseline checks.
6. **Fix broken checks instead of dropping them.** `error` and `invalid` (a guard that fails on base) are bugs in the check.

## Outcomes

| kind | base | PR | outcome | effect |
|---|---|---|---|---|
| discriminating | fail | pass | proven | counts (enables "safe" for the medium tier) |
| discriminating | pass | pass | not-discriminating | does not count |
| discriminating | pass | fail | regression | finding (per `severity`) |
| discriminating | fail | fail | fails-both | needs human |
| guard | pass | pass | holds | supporting only |
| guard | pass | fail | regression | finding (per `severity`) |
| guard | fail | any | invalid | discarded; fix the check |

## Promotion into the baseline

Promote a check when it would catch a *class* of problem on future bumps, for example "regenerated oapi code builds" or "dev-server DB round-trip with the new sqlite". Don't promote checks that are about one version.

1. Copy the script to `scripts/dependency-pr/checks/<id>.sh`. Replace hard-coded versions or packages with values read from `$CLASSIFICATION` (`updates_for gomod`, and so on).
2. Add an entry to `scripts/dependency-pr/checks/registry.json` with `when` (ecosystems), an optional `packages` regex (for example `"^github.com/mattn/go-sqlite3$"`), `on_fail`, `compare_base: true`, and `required`.
3. Promoted checks run on the PR side and are re-run on base when they fail, so they act as guards and pre-existing failures are recognized.
4. Open a normal PR. A human reviews the promotion, never the verifier on its own.
