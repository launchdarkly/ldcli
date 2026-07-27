# Docker test & setup sandbox

A single image (`Dockerfile.test`) that runs the full automated suite and
doubles as a disposable sandbox for manually walking the `ldcli setup` wizard.
The image carries the Go toolchain (for `go test ./...`) and Node (for the
dev-server UI Vitest suite and the wizard's Node SDK path).

## Build

```bash
docker build -f Dockerfile.test -t ldcli-test .
```

## Run the automated tests

```bash
docker run --rm ldcli-test
```

Runs `go test ./...` then the dev-server UI tests. Exit code is non-zero if
anything fails.

## Manually test the setup wizard

The wizard installs SDKs and edits project files, so run it in the throwaway
container rather than on your host. It talks to the LaunchDarkly API, so pass an
access token:

```bash
docker run --rm -it -e LD_ACCESS_TOKEN=<your-token> ldcli-test sandbox
```

You land in `/work/sample-node` (a small Express project). From there:

```bash
ldcli setup            # guided TUI wizard
ldcli setup detect     # detection step only
ldcli setup install    # install the detected SDK only
```

Nothing persists once the container exits.

> Note: this image builds whatever source is checked out. The `setup` command
> lives in the `setup-ld` feature stack, so build from a branch that contains
> it (or from `main` once the stack merges).
