---
name: verify-dependency-pr
description: Verify a Dependabot (or other dependency-update) PR on ldcli the way a careful human reviewer would, and produce a verdict comment (safe to merge / needs human / block). Use to verify, review, triage, or check a dependency bump PR given its number or branch.
---

# Verify a dependency PR

You produce evidence and a verdict comment. You **never approve, request changes, merge, or push to the PR branch.** Verification writes files only; posting the comment is a separate step (`post-comment.sh`) that the caller runs.

All paths are relative to the repo root. Output goes to `.verify-out/pr-<N>/` (gitignored).

## Procedure

### 1. Classify and run the baseline

```bash
scripts/dependency-pr/verify.sh --pr <N>          # or --branch <name>
```

The script is non-interactive. It makes two worktrees: `work/base` (base branch tip) and `work/pr` (PR merged into that tip). It classifies the updates, runs the baseline checks for each affected ecosystem, and writes `result.json`, `comment.md`, and `logs/`. Exit codes: 0 safe, 1 needs attention, 2 infra error. **On 2, fix the environment (missing tool, network) and re-run before going on.** Add `--profile full` when Docker is available and the bump touches CGO/sqlite, the Go directive, or release tooling.

Read `result.json`: `.classification` (updates, `tier`, `tags`, `tier_reasons`), `.checks[]` (`outcome` is `pass`, `fail`, `warn`, `regression`, `changed`, `pre-existing`, `skip`, or `error`), `.reasons`, and `.pre_existing`. Logs for each check are under `logs/<side>/<id>.log`, and evidence (patches, help dumps) is under `state/checks/<id>/<side>/`.

`pre-existing` means the check fails the same way on base. Report it, but it is not this PR's fault. One known case: `go-generate-drift` fails on main because #720 bumped oapi-codegen without regenerating `server.gen.go`, and the regenerated code doesn't compile.

### 2. Impact analysis

Open the playbook for each ecosystem in `.classification.ecosystems`:

| Ecosystem | Playbook |
|---|---|
| gomod | [playbooks/gomod.md](playbooks/gomod.md) |
| npm-ui (`internal/dev_server/ui`) | [playbooks/npm-ui.md](playbooks/npm-ui.md) |
| npm-wrapper (root `package.json`) | [playbooks/npm-wrapper.md](playbooks/npm-wrapper.md) |
| github-actions | [playbooks/github-actions.md](playbooks/github-actions.md) |
| docker | [playbooks/docker.md](playbooks/docker.md) |

For every direct update:

1. **Changelog.** Read the release notes between `from` and `to`: the PR body first, then upstream releases or CHANGELOG (`gh api repos/<o>/<r>/releases`, or `gh release view`). Note breaking changes, new minimum Go/Node versions, new peer requirements, deprecations, and security fixes (GHSA/CVE).
2. **Reach.** Find where the dependency is used: `rg` for imports, plus `go list -deps ./... | rg <module>` and `rg -l '"<module>' --glob '*.go'` for Go, or `rg "from '<pkg>" internal/dev_server/ui/src` for the UI. Map each changelog item to code that actually calls it. Things nothing calls are "unreachable".
3. **Risk.** Start from `.classification.tier`. You may **raise** it (for example, a "patch" that changes behavior you rely on) but never lower it.

Write `agent/impact.json` in the out dir:

```json
{
  "schema": 1,
  "summary": "One paragraph: what changed upstream and what in ldcli it reaches.",
  "tier": "medium",
  "tier_reasons": ["optional: why you raised the tier"],
  "changelog": [{"package": "github.com/mattn/go-sqlite3", "range": "1.14.28..1.14.52", "notes": "…", "breaking": false, "url": "https://…"}],
  "usage": ["internal/dev_server/db/sqlite.go"],
  "findings": [{"severity": "info|warn|block", "text": "…", "evidence": ["url or path"]}],
  "recommendations": ["optional extra suggested actions"]
}
```

`warn` findings make the verdict "needs human" and `block` findings make it "block". Use `block` only for concrete breakage, such as a removed API that ldcli calls.

### 3. Generate targeted checks

Write checks that exercise what this particular update changes, following [reference/generated-checks.md](reference/generated-checks.md). Put them in `generated/` in the out dir (a `checks.json` manifest plus one script per check). Rules that matter:

- A **discriminating** check must **fail on the old version and pass on the new one**. Only those count as proof. A check that passes on both is reported as "does not count".
- A **guard** asserts behavior that must not change (it passes on both). It never counts as proof, but a regression in it is a finding.
- Assert behavior, not version strings. Target code the update actually reaches.

Medium-tier PRs need at least one proven discriminating check, plus the impact review, before they can be "safe to merge". If no meaningful discriminating check exists (say the changes are unreachable from ldcli), say so in `impact.json`, and the verdict stays "needs human". That is fine, because it tells the reviewer exactly what is left to judge.

### 4. Run the generated checks and render

```bash
scripts/dependency-pr/verify.sh --pr <N> --phase generated   # reuses the worktrees and baseline results
```

This runs every generated check on base and on the PR, merges in `agent/impact.json`, recomputes the verdict, and rewrites `comment.md`. If you edit only `impact.json`, use `--phase render`. If a check is broken (an `error`, or `invalid` for a guard that fails on base), fix it and re-run. Don't drop it silently.

### 5. Report

Report the verdict, the reasons, and the path to `comment.md`. Point out pre-existing problems on main separately. Suggest promoting a generated check when it would catch the same class of problem on future bumps (see "Promotion" in the reference).

To post, the caller runs:

```bash
scripts/dependency-pr/post-comment.sh --out-dir .verify-out/pr-<N> --dry-run   # then without --dry-run
```

It edits a single marker comment in place, and it refuses to post if the PR head moved after verification.

## Verdict rules (implemented in `scripts/dependency-pr/lib/verdict.jq`)

- **block**: a check marked `block` regresses (build, tests, `npm ci`, a UI build, regenerated code that doesn't compile, the smoke test), a generated check with `severity: block` regresses, or there is an agent `block` finding.
- **needs-human**: any other failure or warning that isn't pre-existing, a check error, a skipped required check, a high tier, or a medium tier missing the impact review or a proven check.
- **safe-to-merge**: none of the above.

## Don'ts

- Don't fix the PR, for example by rebuilding dist or running tidy. Recommend the fix in the comment instead. Humans decide.
- Don't run checks outside `verify.sh` and then report them as baseline results. Ad-hoc commands belong in generated checks so they run on both sides.
- Don't trust the local environment. `verify.sh` unsets `LD_*` and isolates the XDG dirs, because local credentials make `go test` fail. Generated checks inherit that environment.
