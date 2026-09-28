# Checks

Read only the sections for the checks you picked. For a PR that bumps several
packages, run every check those packages need.

## Contents

- What CI already runs
- How old CI's results are
- Before any Go check
- Before any npm check
- CLI_SMOKE
- STORE_SMOKE
- UI_COMPUTER_USE
- INSTALL_SMOKE
- BUILD_ONLY and TEST_ONLY
- CI_ONLY and NO_EXTRA

## What CI already runs

Read `.github/workflows/` for the current list. The ones that matter here:

- `go.yml` builds ldcli, runs golangci-lint, and runs `go test ./...`. It never
  runs the built binary or starts the dev-server.
- `dev-server-ui.yml` runs lint, Prettier, the Vitest suite, and a production
  build, then fails if any of that changed a checked-in file. It never opens
  the UI in a browser.
- `dependency-scan.yml` lists ldcli's Go and npm dependencies and checks them
  against LaunchDarkly's dependency policy. It doesn't read the Dockerfile. If
  it fails on the PR, include that in the report, after checking whether it
  passes on recent `main` runs (rule 3).

None of them talk to LaunchDarkly.

Only the workflows above, plus the PR title lint, run on pull requests. Check
each workflow's `on:` block rather than trusting this list:
`release-please.yml` runs on pushes to `main`, `manual-publish.yml` only when
someone starts it, and `check-openapi-updates.yml` on a schedule. When a PR
changes one of those, a green CI run says nothing about the change.

The Vitest suite in `internal/dev_server/ui/src/__tests__/` is small. Look at
what it covers before counting `npm test` as coverage of the UI.

## How old CI's results are

CI ran against the `main` the PR was branched from. `prepare-tree.sh` prints
how many commits `main` has moved since. If it's more than a handful, or the
commits touch the same manifests, CI's results describe a tree that no longer
exists: rerun the relevant workflow's steps on the merged tree.

When CI failed, find the step that failed with
`gh run view <run-id> --json jobs`. GitHub deletes old run logs, so
`gh run view --log-failed` can return HTTP 410. The step name is still
available, and rerunning that step locally shows the error.

## Before any Go check

Use the Go version the tree's `go.mod` asks for. If your installed Go is older,
Go tries to download the right one. When `go.mod` names only a minor version
(such as `go 1.25`), that download can fail with "toolchain not available". Set
`GOTOOLCHAIN` to a specific release instead, for example `GOTOOLCHAIN=go1.25.9`.

Run Go tests in a shell where `isolate.sh` is in effect. Several tests in
`cmd/` assume no `LD_` variables are set, and fail or panic when a real
`LD_ACCESS_TOKEN` or `LD_PROJECT` is present.

## Before any npm check

Use the Node version `dev-server-ui.yml` sets up (`node-version`, currently
`lts/*`), because npm versions resolve peer dependencies differently. After
switching, run `node --version` and `npm --version`: another `node` earlier
on `PATH` can win over the one you installed.

Run `npm ci` first. If it fails with `ERESOLVE`, the upgrade is blocked (rule
3). Quote the `While resolving` and `Conflicting peer dependency` lines in the
report, and don't run the rest of the check.

## CLI_SMOKE

For the CLI framework, flag parsing, config, and terminal packages.

    make build
    ./ldcli --help
    ./ldcli --help | cat

Then run `--help` for each command the root help lists, plus
`./ldcli completion bash`, and run `go test ./cmd/...`.

For `viper`, also set one value through an environment variable (for example
`LD_OUTPUT=json`) and one through the temporary config file, and confirm ldcli
picks both up.

Piping output through `cat` only tests what happens when ldcli isn't writing to
a terminal. To test terminal behavior such as help wrapping, output defaults, or
the interactive `setup` flow, run ldcli inside `tmux` or `script -q`. Only
report terminal behavior if you actually ran it that way.

## STORE_SMOKE

For the SQLite driver, the dev-server's HTTP routing, and the LaunchDarkly SDK
packages.

Run `scripts/store-smoke.sh` from the worktree. It builds ldcli and starts the
dev-server on the port `isolate.sh` picked, with a dummy access token. The dummy
token works because the script doesn't pass `--project` or `--source`: nothing
syncs, but the server still opens its databases and serves the UI. The script
checks that:

1. `/ui/` and the `/dev/projects` API both return 200.
2. `dev_server.db` and `dev_server_events.db` exist in the temporary state
   directory.
3. After a restart against the same directory, both still return 200.

If it exits with code 3, CGO or a C compiler is missing, so the SQLite driver
can't be built. Run `go test ./internal/dev_server/...` instead and say so in
the report.

Syncing a real project needs a real token, so SDK bumps always leave that part
unverified. Say so in the report.

## UI_COMPUTER_USE

For packages that ship in the dev-server UI bundle.

    cd internal/dev_server/ui
    npm ci
    npm test
    npm run build
    npm run prettier:write
    git status --short

The ldcli binary serves the `dist/` folder checked into the repo, and Dependabot
doesn't rebuild it. If the build changed `dist/`, or Prettier changed any file,
the PR won't pass CI as opened and won't ship the new version. That's the
"needs another commit" case in rule 3. Don't commit the rebuild yourself.

To test what would ship once that commit lands, keep your local `dist/` and run
`scripts/store-smoke.sh` from the worktree root to confirm the server starts.
Then start the server yourself the same way (`./ldcli dev-server start --port
"$SMOKE_PORT" --access-token dummy-for-local-smoke`), open the UI, and visit
each page: Flags, Events, and Debug sessions. A blank page, an error overlay,
missing navigation, or unstyled components means ldcli failed with the upgrade.

`npm run dev` doesn't count, because it serves a different build from the one
ldcli ships.

## INSTALL_SMOKE

For the npm package at the repo root.

    npm pack
    npm install -g --prefix "$SMOKE_DIR/npm" ./launchdarkly-ldcli-*.tgz
    "$SMOKE_DIR/npm/bin/ldcli" --version

The install step downloads the release binary that's already published for the
version in `package.json`. This tests the npm wrapper, not the Go code in the
PR, so say that in the report.

## BUILD_ONLY and TEST_ONLY

For build tools, linters, test libraries, and mock generators.

Run only the tool that changed, for example `npm run build`, `npm run lint`,
`npm test`, or `go test ./...`. For a mock generator bump, run one
`go generate` command and confirm the generated files don't change. For a code
generator bump (`oapi-codegen`, `kin-openapi`), escalate if the generated files
would change. Don't open a browser.

For a UI build tool (`vite`, `rollup`, their plugins), run `npm run build` and
then `git status --short`. If `dist/` changed, the PR needs a rebuilt `dist/`
before it can land (rule 3).

## CI_ONLY and NO_EXTRA

For GitHub Actions, the Docker base image, and packages ldcli doesn't import
directly.

Don't start ldcli, except inside the Docker image below. Read the release notes
and the workflow or Dockerfile. Never trigger a release workflow.

For an Actions bump:

- Check the `on:` block of every workflow the PR changes. If none of them run on
  pull requests, CI didn't exercise the change; say so, and list the first real
  run (the next push to `main`, or the next release) under what's still
  unverified.
- Compare the inputs each workflow passes (`with:`) against the new version's
  `action.yml`, and look for changed defaults such as the Node runtime. For a
  SHA-pinned action, read `action.yml` at the new SHA.
- If `actionlint` is installed, run it on the changed workflows.

For a Docker base image bump, if Docker is available, build the image and run
it once. The Dockerfile copies a prebuilt `ldcli`, so build that first, and
pass the opt-outs in, because the container doesn't inherit your shell:

    make build
    docker build -f Dockerfile.goreleaser -t ldcli-verify .
    docker run --rm -e LD_ANALYTICS_OPT_OUT=true -e LD_UPDATE_CHECK_OPT_OUT=true ldcli-verify --version
