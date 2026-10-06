---
name: verify-dependency-pr
description: Verify a Dependabot (or other dependency-update) PR on ldcli to the standard of a diligent reviewer, and produce a verdict comment (safe to merge, needs a human decision, block, or incomplete). Use to verify, review, triage, or check a dependency update PR, given its number or branch.
---

# Verify a dependency PR

You verify the PR to the standard of a diligent reviewer. After your run, a person must not have to verify anything again. A person only answers the decisions that your comment asks for.

You do not approve, request changes, merge, or push to the PR branch. Verification writes files only. The caller runs `post-comment.sh` to post the comment.

All paths are relative to the repository root. Output goes to `.verify-out/pr-<N>/`, which git ignores.

## Verdicts

| Verdict | Meaning | Exit code |
|---|---|---|
| `block` | The PR must not merge as it is. The comment gives the fix. | 1 |
| `needs-human` | Verification is complete. A person must answer one or more specific questions. | 1 |
| `incomplete` | A required check, gate, or review did not run, or proved nothing. A rerun or more agent work closes it, not a person. | 2 |
| `safe-to-merge` | Every required check and review ran and passed. | 0 |

A gate is a check that must pass, for example the build or the tests. A failure that also happens on base is "pre-existing" and does not count against the PR. Some checks list separate findings: actionlint, golangci-lint, prettier, and `npm ls`. If each finding of the PR also occurs on base, the failure is pre-existing. A pre-existing failure never satisfies a gate.

## The diligent reviewer standard

This table shows what must be true before the PR can be safe to merge. The tier comes from `result.json` (`.tier`). You can raise the tier, but you cannot lower it.

| Tier | Typical updates | Required |
|---|---|---|
| low | A patch of a direct dependency, a dev dependency, or a transitive-only change | The baseline checks pass. |
| medium | A minor update of a runtime dependency, or any update in a risk area (CGO, LaunchDarkly SDKs, CLI libraries, UI framework, Docker base image) | The low items, plus an impact review of each direct update. If upstream behavior changes reach ldcli, at least one generated check must prove the change. |
| high | A major update (including a 0.x minor), a code generator, release tooling, a go directive change, or more than three direct updates | The medium items, plus each breaking change mapped to the ldcli code that it touches. |

The baseline checks cover each ecosystem: build, tests, generated code, UI dist, smoke tests, transitive changes, licenses, upstream notes, and Actions interfaces. The playbooks list them.

## Procedure

### 1. Run the baseline

```bash
scripts/dependency-pr/verify.sh --pr <N>          # or --branch <name>
```

The script makes two worktrees. `work/base` is the tip of the base branch. `work/pr` is the PR merged into that tip. The script classifies the updates, runs the baseline checks, and writes `result.json`, `comment.md`, and `logs/`. If Docker is available and the update touches CGO, the go directive, or release tooling, add `--profile full`.

Read these fields in `result.json`:

- `.classification`: the updates, the tier, the risk tags, and the reasons for the tier.
- `.blocks`, `.decisions`, `.incomplete`: the items that set the verdict.
- `.checks[]`: the outcome of each check. The log of a check is in `logs/<side>/<id>.log`. Its evidence is in `state/checks/<id>/<side>/`.
- `.fixes`: machine-applicable fix recipes. Do not apply them. A later step will.

If the exit code is 2 because a tool is missing, install the tool and run the script again.

### 2. Do the impact review (medium and high tier)

Open the playbook for each ecosystem in `.classification.ecosystems`:

| Ecosystem | Playbook |
|---|---|
| gomod | [playbooks/gomod.md](playbooks/gomod.md) |
| npm-ui (`internal/dev_server/ui`) | [playbooks/npm-ui.md](playbooks/npm-ui.md) |
| npm-wrapper (root `package.json`) | [playbooks/npm-wrapper.md](playbooks/npm-wrapper.md) |
| github-actions | [playbooks/github-actions.md](playbooks/github-actions.md) |
| docker | [playbooks/docker.md](playbooks/docker.md) |

For each direct update, do these steps:

1. Read the upstream notes. The `upstream-changes` check saves them in `state/checks/upstream-changes/pr/notes/`, and its details give the compare link. For an npm package in a monorepo, the notes come from the package `CHANGELOG.md`. For `golang.org/x/*` modules, the notes are the commit messages. Also read the PR body. If the `pr-disclosure` check names direct updates that the body does not name, review those updates the same as the others. If no notes exist, read the upstream diff from the compare link.
2. Find breaking changes, security fixes, deprecations, new minimum versions (Go, Node, runner), new peer requirements, and license changes.
3. Find where ldcli uses the dependency. For Go, use `rg -l '"<module>' --glob '*.go'` and `go list -deps ./... | rg <module>`. For the UI, use `rg "from '<pkg>" internal/dev_server/ui/src`.
4. Map each upstream change to the ldcli code that it reaches. A change that no ldcli code reaches is "not reachable".
5. Read the `transitive-changes` and `license-changes` details. Explain each new module, major transitive update, and license change.

Write `agent/impact.json` in the output directory:

```json
{
  "schema": 1,
  "summary": "What changed upstream, and what in ldcli it reaches.",
  "tier": "medium",
  "tier_reasons": ["Only if you raise the tier: the reason"],
  "changelog": [{"package": "github.com/mattn/go-sqlite3", "range": "1.14.28..1.14.52", "notes": "…", "breaking": false, "url": "https://…"}],
  "usage": ["internal/dev_server/db/sqlite.go"],
  "behavior_changes_reachable": true,
  "no_local_proof": "Only if no local check can run the changed code: the reason",
  "findings": [
    {"severity": "info", "text": "A fact for the reader.", "evidence": ["url or path"]},
    {"severity": "decide", "question": "Accept X for Y?", "text": "Why only a person can decide this.", "evidence": ["…"]},
    {"severity": "block", "text": "Concrete breakage, for example a removed API that ldcli calls.", "evidence": ["…"]}
  ]
}
```

The verdict is `incomplete` if `changelog` does not name every direct update, or if `behavior_changes_reachable` is missing.

Sometimes no local check can run the changed code. An example is an action that only release workflows use. In that case, set `no_local_proof` to the reason. The verdict then asks a person to accept the change without a local proof. Do not use `no_local_proof` to avoid a check that you can write.

### 3. Write generated checks

If `behavior_changes_reachable` is true, write at least one discriminating check. Follow [reference/generated-checks.md](reference/generated-checks.md). Put the checks in `generated/` in the output directory. The important rules:

- A discriminating check must fail on the old version and pass on the new version. Only such a check counts as proof.
- A guard asserts behavior that must not change. It passes on both versions. A guard never counts as proof, but a guard that fails on the PR blocks the PR.
- Assert behavior, not version strings. Test code that the update reaches.

### 4. Run the generated checks

```bash
scripts/dependency-pr/verify.sh --pr <N> --phase generated
```

This command runs each generated check on base and on the PR. It reads `agent/impact.json`, calculates the verdict again, and writes `comment.md` again. If you change only `impact.json`, use `--phase render`. If a generated check is `error`, `invalid`, or `fails-both`, fix it and run the phase again.

### 5. Report

Report the verdict, the blocks, the decisions, the incomplete items, and the path to `comment.md`. Give pre-existing problems on main separately. A generated check can find the same type of problem in future updates. In that case, recommend it for the baseline (see "Promotion" in the reference).

To post, the caller runs:

```bash
scripts/dependency-pr/post-comment.sh --out-dir .verify-out/pr-<N> --dry-run   # then without --dry-run
```

The script edits one comment in place. It does not post if the PR head moved after verification.

## When to ask a person

Ask a person only when the evidence is complete and the next step is a choice. Each decision is one question that a person can answer with yes or no. Put the evidence next to it.

These items are decisions:

- Accept a change that no automated check can run, for example "Accept actions/checkout v6 in release workflows that PR CI does not run?"
- Accept a new license, a license change, or a new npm install script.
- Accept a visible change to the CLI, for example different help text or flags.
- Choose between options, for example "Remove the unused react-window instead of updating it?"

These items are not decisions:

- A stale dist, untidy `go.mod`, or generated code that is out of date. These are blocks with a fix.
- A check that did not run because a tool is missing. This item is incomplete.
- A breaking change that you have not mapped yet. Do the mapping.

## Rules

- Do not fix the PR yourself. Each failing check records a fix recipe in `result.json` for a later step.
- Do not run ad hoc commands and report them as baseline results. Put them in generated checks, so that they run on both versions.
- Do not trust the local environment. `verify.sh` removes `LD_*` variables and uses private XDG directories, because local credentials make `go test` fail. Generated checks get the same environment.
- A check that fails because of a tool or environment problem must report `incomplete`, not `fail`.
