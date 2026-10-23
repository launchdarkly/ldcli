# Playbook: npm wrapper package (root `package.json`)

The root package publishes `@launchdarkly/ldcli` to npm. Its only dependency is `@go-task/go-npm`. The `postinstall` script of go-npm downloads the GitHub release archive that `goBinary.url` names, for the version in `package.json`. PR CI does not test this path.

## Diligent reviewer standard

1. The package installs with the locked go-npm version, and the postinstall script installs a binary that runs and reports the package version.
2. The packed contents (`npm pack`) contain only the expected files.
3. The upstream changes of go-npm are known: URL templates, archive formats, platform names, and `engines`.
4. The license of go-npm is in the policy.

## What the baseline checks

`npm-wrapper-install` (gate) covers items 1 and 2. It installs into a scratch directory with scripts on, as users do, and with a scratch `npm_config_prefix`. Newer npm versions do not have `npm bin`, so go-npm copies the binary into `$npm_config_prefix/bin`. The check needs network access to npm and GitHub. `upstream-changes` collects the notes for item 3. `license-changes` covers item 4.

## What the agent must do

- npm does not publish the lockfile. Users get the go-npm version that the range in `package.json` allows (`^0.2.0`). A change to the lockfile only changes what CI and the check install. Only a range change in `package.json` reaches users. State which case applies.
- Make sure that the archive names from `.goreleaser.yaml` (`archives.name_template`) still match what go-npm expects.
- The release publishes through `scripts/publish-npm.sh` and `.github/actions/publish-npm`, with npm trusted publishing (npm 11.5.1 or later). Make sure that the update needs no change to the publish flags.

## When to ask a person

The tier is high by default, because every npm user runs this code at install time. If the evidence is complete and nothing failed, the PR can be safe to merge. Ask a person only if go-npm changes what users get, for example a new install location or a new platform mapping.
