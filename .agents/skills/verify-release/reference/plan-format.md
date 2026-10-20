# Plan and check format

You write three things in the out dir: `agent/plan.json`, `generated/checks.json`, and one script in `generated/` for each check. `scripts/release/lib/plan.py validate --out-dir <out dir>` tells you what is missing.

## agent/plan.json

```json
{
  "schema": 1,
  "summary": "Two or three sentences about the release and the main risks.",
  "units": [
    {
      "id": "dev-server",
      "title": "Dev server: listen address and request limits",
      "commits": ["1854c86", "e381109"],
      "risk": "high",
      "risk_reasons": ["Opens a port on the machine of the user."],
      "claims": [
        {
          "id": "loopback-default",
          "text": "With no --host flag, the dev server accepts connections only on 127.0.0.1.",
          "source": "#845 title and cmd/dev_server/start.go",
          "checks": ["dev-server-loopback-default"]
        },
        {
          "id": "release-workflow-runs",
          "text": "The release workflow still creates the GitHub release with release-please-action v5.",
          "source": "#722",
          "no_local_proof": "Only the release run executes this workflow. actionlint passes in the baseline."
        }
      ],
      "findings": [
        {"severity": "info", "text": "A fact for the reader.", "evidence": ["path or URL"]},
        {"severity": "decide", "question": "Ship X without Y?", "text": "Why a person must decide.", "evidence": ["…"]},
        {"severity": "block", "text": "What is broken.", "fix": "What to do.", "evidence": ["…"]}
      ],
      "not_verified": "Optional: what the checks do not cover, and why."
    }
  ]
}
```

- `id`: kebab-case, unique.
- `commits`: short SHAs from `inventory.json`. Each commit in the release (except the release commit) is in exactly one unit.
- `risk`: `low`, `medium`, or `high`. It must not be lower than the highest risk of its commits in the inventory.
- `claims[].checks`: the ids of the checks that prove the claim. A claim without checks needs `no_local_proof`, and the report then asks a person to accept it.
- `claims[].baseline`: optional. Ids of baseline checks in `scripts/release/checks/registry.json` that prove the claim, for example `["docker-image"]`. If the baseline check passes, it counts as proof. Do not write a check that repeats a baseline check.
- `findings`: a `block` finding needs `fix`. A `decide` finding needs a `question` that ends with a question mark.

## agent/units/&lt;id&gt;.json

Parallel subagents must not edit the same file. Give each subagent one or more units. A subagent writes the claims, the findings, and the checks of its unit in `agent/units/<unit id>.json`. It does not edit `agent/plan.json`. The tools merge the files.

```json
{
  "risk": "high",
  "risk_reasons": ["Optional: more reasons."],
  "claims": [ … ],
  "findings": [ … ],
  "not_verified": "…",
  "checks": [ … the same objects as in generated/checks.json; "unit" is optional … ]
}
```

## generated/checks.json

```json
{
  "schema": 1,
  "checks": [
    {
      "id": "dev-server-loopback-default",
      "unit": "dev-server",
      "claims": ["loopback-default"],
      "title": "The dev server does not accept connections on a non-loopback address by default",
      "kind": "discriminating",
      "adversarial": false,
      "revert": ["1854c86"],
      "script": "dev-server-loopback-default.sh",
      "rationale": "cmd/dev_server/start.go sets the default host. The check starts the server and connects through the address of the machine.",
      "timeout": 300
    }
  ]
}
```

- `id`: kebab-case, unique. Start it with the unit id.
- `kind`: `discriminating` or `guard` (see the table).
- `adversarial`: set it to true for a check that tries to break the change. Each high-risk unit needs one.
- `revert`: optional. Commits to revert from the candidate for a third run. Use it for a fix or a change inside a group of commits. If the revert conflicts, the proof compares with the last release only, and the report says so.
- `timeout`: seconds. The default is 600.

## Outcomes

| Kind | Last release | Candidate | Reverted | Outcome | Effect |
|---|---|---|---|---|---|
| discriminating | fail or skip | pass | not run | proven | Proof |
| discriminating | fail or skip | pass | fail or skip | proven-by-revert | Proof |
| discriminating | fail or skip | pass | pass | not-discriminating | No proof |
| discriminating | pass | pass | any | not-discriminating | No proof |
| discriminating | pass | fail | any | regression | Block |
| discriminating | fail or skip | fail | any | fails-both | Incomplete. If the judge finds a bug, block. |
| guard | pass or skip | pass | any | holds | Proof |
| guard | pass or skip | fail | any | regression | Block |
| guard | fail | any | any | invalid | Incomplete |
| any | any | error, incomplete, or skip | any | incomplete | Incomplete |

The judge can remove a proof (`vacuous`) or a block (`false-positive`). See [judge.md](judge.md).

## Script contract

The runner runs each script with `SIDE=base` (the last release) and `SIDE=pr` (the candidate). For a check with `revert`, it also runs the script with `SIDE=revert`. `WT` is the worktree for that side. The working directory is `$WT`. The `LD_*` variables are not set. `HOME` and the XDG directories are private.

1. Start with these two lines:

   ```bash
   source "$VERIFY_ROOT/lib/check.sh"
   source "$RELEASE_ROOT/lib/unit.sh"
   ```

2. Work in `$WT` only. Write temporary files in `$ARTIFACTS`. If the script changes the worktree, call `restore_tree` before it finishes.
3. Finish with exactly one of `pass "summary"`, `fail "summary"`, `skip "reason"`, or `incomplete "reason"`. If the last release does not have the feature, use `skip`. If a tool or the network fails, use `incomplete`.
4. Add evidence with `detail "markdown line"` and `detail_block <file> [lines]`. If the check fails, add the fix with `recommend "text"`.
5. Make the check deterministic. Wait for a condition, not for a fixed time. Do not depend on the clock.

Give helper files the extension `.txt`, so that Go tools do not read them from `generated/`.

### Example: a CLI check against the fake API

```bash
#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
source "$RELEASE_ROOT/lib/unit.sh"
bin=$(ldcli_bin) || incomplete "build failed"
"$bin" sync prompts --help >/dev/null 2>&1 || skip "this version has no sync prompts command"
fake_api_start "$(dirname "$0")/sync-routes.json.txt" || incomplete "the fake API did not start"
out=$("$bin" sync prompts --dry-run --base-uri "$FAKE_API_URI" --access-token placeholder 2>&1)
echo "$out"
writes=$(jq -s '[.[] | select(.method != "GET")] | length' "$FAKE_API_RECORD")
detail "- requests: $(jq -s 'length' "$FAKE_API_RECORD"), writes: $writes"
[ "$writes" -eq 0 ] || fail "--dry-run sent $writes write request(s)"
pass "--dry-run sent no write request"
```

### Example: a Go test against a package

Copy a test file into the package, run only that test, and delete it.

```bash
#!/usr/bin/env bash
source "$VERIFY_ROOT/lib/check.sh"
dst="$WT/internal/dev_server/zz_verify_body_limit_test.go"
cp "$(dirname "$0")/body_limit_test.go.txt" "$dst"
trap 'rm -f "$dst"' EXIT
out=$(go test -count=1 -run '^TestVerifyBodyLimit$' ./internal/dev_server/ 2>&1); rc=$?
echo "$out"
grep -q 'no test files\|undefined:' <<<"$out" && skip "this version does not have the code under test"
[ $rc -eq 0 ] && pass "the server rejects a 20 MB body with 413"
fail "$(tail -n 3 <<<"$out")"
```
