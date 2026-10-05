# Playbook: GitHub Actions (`.github/workflows`, `.github/actions`)

## Baseline (from `verify.sh`)

- `actions-pinning` enforces SEC-7924 (#668). Third-party actions must be pinned to a 40-character commit SHA with a `# vX.Y.Z` comment. Actions owned by `actions/`, `github/`, and `launchdarkly/` are exempt; that matches the current repo.
- `actionlint` (v1.7.7, through `go run` when it isn't installed).
- `actions-coverage` lists every usage of each bumped action and whether the calling workflow triggers on `pull_request`. Composite actions are traced back to their callers. For majors it fetches upstream `action.yml` at both refs and compares inputs: inputs that were removed but are still passed, new required inputs, and runtime changes (for example `node20 → node24`).

## Key fact

**A green PR only proves something about workflows that run on `pull_request`.** On this repo that is `go.yml`, `dev-server-ui.yml`, `dependency-scan.yml`, and `lint-pr-title.yml`. `release-please.yml`, `manual-publish.yml`, `check-openapi-updates.yml`, and the `publish` / `publish-npm` composites never run on a PR, so a bump there is unverified by CI (#722 release-please v5, #718).

## Impact analysis

- Read the release notes for every major between `from` and `to` (`gh release list -R <owner>/<repo>`, `gh release view <tag> -R …`). Note runtime requirements (node24 needs runner ≥2.327.1, which GitHub-hosted runners satisfy), changed defaults (for example `actions/checkout`'s `persist-credentials` handling), and removed or renamed inputs and outputs.
- Check the outputs the repo consumes (`steps.<id>.outputs.*`). The input diff doesn't cover outputs: `rg 'steps\.[a-z_-]+\.outputs' .github`.
- Dependabot doesn't scan `.github/actions/*` composites, so a bump to `actions/checkout` leaves the composites on the old version. Note any version skew this creates.
- release-please majors: compare `release-please-config.json` and `.release-please-manifest.json` against the new version's config schema, and the outputs used in `release-please.yml` (`release_created`, `tag_name`, …).

## Generated-check ideas

- Guard: the workflow's inputs and outputs used by later steps still exist in the new `action.yml` (when `actions-coverage` could not fetch it, do this by hand with `gh api`).
- Discriminating, for a bug-fix bump: if the old version had a known bug that the repo works around, check that the workaround is now unnecessary (and recommend removing it).
- No local check can run a release workflow. Recommend a `manual-publish` dry run (`dry-run: true`) after merge, or before it on a branch, as the reviewer's action.

Majors are high tier, which means "needs human". Minor or patch bumps of actions used only by PR-triggered workflows can be "safe to merge" once CI on the PR is green.
