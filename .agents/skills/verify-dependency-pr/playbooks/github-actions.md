# Playbook: GitHub Actions (`.github/workflows`, `.github/actions`)

## Diligent reviewer standard

1. Each third-party action uses a full commit SHA with a version comment (SEC-7924, #668).
2. The workflows pass `actionlint`.
3. For each updated action, the inputs that the repository passes still exist, and no new required input is missing. The outputs that later steps read still exist.
4. Changed input defaults, the runtime (for example `node20` to `node24`), and the runner requirements are known.
5. The `permissions` blocks of the workflows did not change, or the change is approved.
6. PR CI runs each workflow that uses the action. For a workflow that runs only at release, a person accepts the risk.
7. The upstream release notes between the two versions are known.

## What the baseline checks

| Statement | Check |
|---|---|
| 1 | `actions-pinning`. Actions from `actions/`, `github/`, and `launchdarkly/` are exempt, as in the current repository. |
| 2 | `actionlint` (v1.7.7, installed through `go install` when it is missing) |
| 3, 4, 5 | `actions-coverage`. It reads the upstream `action.yml` at both refs, traces composite actions back to the workflows that call them, and compares the `permissions` blocks on base and PR. If the interface does not match, the PR is blocked. |
| 6 | `actions-coverage`. A breaking update that a workflow without a `pull_request` trigger uses becomes a decision. |
| 7 | `upstream-changes` |

PR CI runs only `go.yml`, `dev-server-ui.yml`, `dependency-scan.yml`, and `lint-pr-title.yml`. `release-please.yml`, `manual-publish.yml`, `check-openapi-updates.yml`, and the `publish` and `publish-npm` composite actions do not run on a PR.

## What the agent must do

- Read the release notes of each major version in the range. Note new runner minimum versions, changed defaults, and changed credential or token behavior.
- Look at the steps that use the action in workflows that PR CI does not run. Make sure that their inputs, outputs, and side effects (for example `git push` after `actions/checkout`) still work. Put each check that you can write as a guard in `generated/`.
- Dependabot does not scan the composite actions in `.github/actions/`. Note any version difference that the update creates between a workflow and a composite action.
- For release-please majors, compare `release-please-config.json` and `.release-please-manifest.json` with the configuration schema of the new version, and the outputs that `release-please.yml` reads.

## When to ask a person

- A breaking update is used in workflows that PR CI does not run. Ask: "Accept <action> <version> in <workflows>, which PR CI does not run?" Give the interface comparison, the runtime change, and the guard results as evidence.
- A workflow `permissions` block changed.
- A non-breaking update changes the default of an input that the repository does not set.

No local check can run a release workflow. The comment can recommend a `manual-publish` dry run (`dry-run: true`) as follow-up.
