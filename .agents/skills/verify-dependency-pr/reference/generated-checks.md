# Generated checks: format and rules

A generated check is a check that the agent writes for one PR. The runner runs it on the base version and on the PR version.

## Layout

```
.verify-out/pr-<N>/generated/
  checks.json          manifest
  <id>.sh              one script for each check
```

```json
{
  "schema": 1,
  "checks": [
    {
      "id": "events-query-cancellation-overhead",
      "title": "Debug-events query adds no per-row allocations under a request context",
      "kind": "discriminating",
      "rationale": "go-sqlite3 1.14.51 stopped starting a goroutine and a channel for each row when ctx.Done() != nil. events_db.QueryEvents reads rows under the HTTP request context.",
      "script": "events-query-cancellation-overhead.sh",
      "timeout": 300
    }
  ]
}
```

- `id`: kebab-case, unique.
- `kind`: `discriminating` (expected to fail on base and pass on the PR) or `guard` (expected to pass on both).
- `timeout`: seconds. The default is 600.

## Script contract

The runner runs each script two times: one time with `SIDE=base` and `WT=<base worktree>`, and one time with `SIDE=pr` and `WT=<pr worktree>`. In both runs, the working directory is `$WT`, the `LD_*` variables are not set, and the XDG configuration, state, and data directories are private. Each script must obey these rules:

1. Start with `source "$VERIFY_ROOT/lib/check.sh"`.
2. Work on `$WT` only. Write temporary files in `$ARTIFACTS`, not in the worktree. If the script changes the worktree, call `restore_tree` before it finishes.
3. Finish with one of `pass`, `fail`, `incomplete`, or `skip`. If a tool is missing or the network fails, use `incomplete`, not `fail`. If the script stops without one of these (a crash or a timeout), the runner records `error`.

Helpers: `detail "markdown line"` (shown in the comment), `detail_block file [lines]`, `recommend "action"`, `run cmd…` (writes the command to the log), `build_ldcli <path>`, `ensure_ui_deps`, `free_port`, `updates_for <ecosystem>`, `$UI_DIR_REL`.

For Go behavior, a temporary test file works best. Copy it into the package under test, run only that test, and then delete it. This is the check for go-sqlite3 1.14.52 (#829). It measured 2.03 extra allocations for each row on the old version and 0.03 on the new version, so it is proven:

```bash
#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
dst="$WT/internal/dev_server/events_db/zz_verify_alloc_test.go"
cp "$(dirname "$0")/events_query_alloc_test.go.txt" "$dst"
trap 'rm -f "$dst"' EXIT
out=$(cd "$WT" && go test -count=1 -run '^TestVerifyQueryEventsCancellationOverhead$' -v ./internal/dev_server/events_db/ 2>&1)
rc=$?
echo "$out"
measured=$(grep -m1 '^allocs:' <<<"$out")
[ -n "$measured" ] || fail "test did not run: $(tail -n1 <<<"$out")"
[ $rc -eq 0 ] && pass "no per-row allocations under a cancellable context ($measured)"
fail "allocates per row under a cancellable context ($measured)"
```

The test runs `QueryEvents` on 500 rows two times: one time with `context.Background()` and one time with a context that can be cancelled. It uses `testing.AllocsPerRun`, and it fails if the difference is more than 0.5 allocations for each row. Measure a difference against a control, not an absolute number. Then unrelated allocations cancel out.

Give helper files the extension `.txt`, so that Go tools do not read them from the generated directory.

For CLI behavior, build with `build_ldcli "$ARTIFACTS/ldcli"` and test the output. For the dev server, start it as `binary-smoke.sh` does: a free port, `--access-token verify-smoke-placeholder`, and `XDG_STATE_HOME` in `$ARTIFACTS`.

## Rules

1. A check must fail on the old version before it counts. The runner applies this rule. A discriminating check counts only with the outcome `proven`. If it passes on both versions, it does not count. Then the check probably does not reach the changed code. Change the check.
2. Test behavior, not versions. `sqlite_version() >= 3.50` or "package.json says 2.3.2" proves nothing about ldcli. Run code paths that ldcli uses.
3. Name the ldcli code path that the check covers in `rationale`. If the changed code is not reachable from ldcli, set `behavior_changes_reachable` to false in `impact.json`, and give the reason.
4. Make each check deterministic. Do not use sleeps to wait for events. Do not depend on the clock. Do not use the network, unless the summary says so. Two runs must give the same result.
5. Generated checks add to the baseline. They do not replace or skip baseline checks.
6. Fix a broken check. Do not delete it. The outcomes `error`, `invalid`, and `fails-both` make the verdict `incomplete`.

## Outcomes

| Kind | Base | PR | Outcome | Effect on the verdict |
|---|---|---|---|---|
| discriminating | fail | pass | proven | Counts as proof of a reachable change |
| discriminating | pass | pass | not-discriminating | Does not count |
| discriminating | pass | fail | regression | block |
| discriminating | fail | fail | fails-both | incomplete: fix the check or the analysis |
| guard | pass | pass | holds | Supports the review, but is not proof |
| guard | pass | fail | regression | block |
| guard | fail | any | invalid | incomplete: fix the check |
| any | error, skip, or incomplete | any | error or incomplete | incomplete |

## Promotion into the baseline

Promote a check if it can find the same type of problem in future updates. Examples are "the regenerated oapi code builds" and "the dev-server database works with the new SQLite". Do not promote a check that is about one version.

1. Copy the script to `scripts/dependency-pr/checks/<id>.sh`. Replace fixed versions and package names with values from `$CLASSIFICATION` (`updates_for gomod`, and so on).
2. Add an entry to `scripts/dependency-pr/checks/registry.json` with `when` (ecosystems), an optional `packages` regex (for example `"^github.com/mattn/go-sqlite3$"`), `compare_base: true`, `required`, and `gate` if the check must pass.
3. If a failure has a mechanical fix, record it with `fix_recipe`.
4. Open a normal PR. A person reviews the promotion.
