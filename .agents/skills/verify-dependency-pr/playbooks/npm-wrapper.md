# Playbook: npm wrapper package (root `package.json`)

The root package publishes `@launchdarkly/ldcli` to npm. Its only dependency is `@go-task/go-npm`, whose `postinstall` downloads the GitHub release tarball named by `goBinary.url` for the version in `package.json`. Nothing in PR CI exercises it.

## Baseline (from `verify.sh`)

`npm-wrapper-install` runs `npm ci` in a scratch copy, with install scripts enabled the way users get them. It then checks that `bin/ldcli --version` reports the package version, and records the `npm pack --dry-run` file list. It needs network access (the npm registry and GitHub releases).

## Impact analysis

- **The lockfile is not published.** Users get whatever go-npm version satisfies the `package.json` range (`^0.2.0`). A lockfile-only bump changes what CI and the check install, but not what users install. Only a range change in `package.json` reaches users.

- Read the go-npm changelog. Look at changes to `goBinary` templating (`{{version}}`, `{{platform}}`, `{{arch}}`), the archive formats it extracts, the platform and arch names it maps (darwin/linux/windows, amd64/arm64/386), and its `engines`.
- Check that the release asset names produced by `.goreleaser.yaml` (`archives.name_template`) still match what the new go-npm expects.
- Publishing goes through `scripts/publish-npm.sh` and `.github/actions/publish-npm` with npm trusted publishing (npm ≥11.5.1). The bump must not need different publish flags.

## Generated-check ideas

- Discriminating, when the bump fixes a platform: run the postinstall with the platform overridden (if go-npm supports that through env vars) and assert the URL it resolves.
- Guard: `npm uninstall` (the `preuninstall` hook) removes `bin/ldcli` cleanly from the scratch install.

High tier by default (`risk-map.json`): every npm user runs this code at install time. Expect "needs human" unless the change is a trivial patch.
