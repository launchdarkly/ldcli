# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Project Overview

LaunchDarkly CLI (`ldcli`) — a Go CLI for managing LaunchDarkly feature flags. Built with Cobra/Viper, distributed via Homebrew, Docker, NPM, and GitHub Releases.

## Common Commands

```bash
make build              # Build binary as ./ldcli
make test               # Run all tests (go test ./...)
go test ./path/to/pkg   # Run tests for a specific package
make generate           # Regenerate code from OpenAPI spec (go generate ./...)
make vendor             # Tidy and vendor dependencies
make install-hooks      # Install git pre-commit hooks
make openapi-spec-update # Download latest OpenAPI spec and regenerate code
```

## Code Generation

Resource commands are auto-generated from the LaunchDarkly OpenAPI spec (`ld-openapi.json`):

- **Generator:** `cmd/resources/gen_resources.go` (build tag: `gen_resources`)
- **Template:** `cmd/resources/resource_cmds.tmpl`
- **Output:** `cmd/resources/resource_cmds.go` (~613KB, do not edit manually)
- **Trigger:** `//go:generate` directive in `cmd/root.go`

The dev server API is also generated: `internal/dev_server/api/server.gen.go` (via oapi-codegen).

## Architecture

**Entry point:** `main.go` → `cmd.Execute(version)` → `cmd/root.go` (Cobra root command)

**Command layer (`cmd/`):**
- Each subcommand (flags, members, config, login, dev-server, sourcemaps, resources) has its own package
- Resource commands are generated; custom commands are hand-written
- Analytics tracking via `PersistentPreRun` hooks

**Internal packages (`internal/`):**
- Each domain package (flags, environments, members, projects, resources, dev_server) exposes a `Client` interface for dependency injection
- `internal/dev_server/` — local dev server with SQLite storage, embedded React UI, and LaunchDarkly SDK integration
- `internal/config/` — manages CLI configuration via `$XDG_CONFIG_HOME/ldcli/config.yml`
- `internal/output/` — response formatting (JSON/plaintext)

**Configuration precedence:** CLI flags → environment variables (prefix `LD_`) → config file

## Adding a New Command

1. Add command to root via `cmd.AddCommand` in `NewRootCommand()` in `cmd/root.go`
2. Update usage template in `getUsageTemplate()` in `cmd/root.go`
3. Add analytics instrumentation via `PersistentPreRun` calling `tracker.SendCommandRunEvent`

## Dev Server Frontend

Located at `internal/dev_server/ui/` — React 18 + TypeScript + Vite, embedded into the Go binary.

```bash
cd internal/dev_server/ui
npm ci
npm test        # Vitest
npm run lint    # ESLint
npm run build   # Production build (checked into repo)



```

## Testing

- Go tests use `testify` for assertions and `go.uber.org/mock` for mocking
- Mock generation via `mockgen`
- Test data in `cmd/resources/test_data/` and `cmd/config/testdata/`

## Pre-commit Hooks

Installed via `make install-hooks`. Checks:
- `go fmt` formatting
- `go.mod`/`go.sum` tidiness
- Dev server UI tests and build (requires npm)

## Linting

- Go: `golangci-lint` (v1.63.4) via pre-commit
- Frontend: ESLint + Prettier

## Cursor Cloud specific instructions

The Cloud Agent environment ships Go 1.25 (required by `go.mod`; installed at `/usr/local/go`) and Node 22, and pre-builds the `ldcli` binary plus the dev-server UI `node_modules`.

**LaunchDarkly targeting via injected secrets.** The environment injects these as env vars so `ldcli` auto-targets a LaunchDarkly staging instance and project with no flags or config file (Viper reads them natively):
- `LD_ACCESS_TOKEN` — API access token
- `LD_BASE_URI` — instance base URI (maps to the `base-uri` setting)
- `LD_PROJECT` — default project key (maps to the `project` setting)

With these set, live read-only commands work directly, e.g. `./ldcli projects get`, `./ldcli environments list`, `./ldcli flags list`. `dev-server` can also start since a token is present. Do not write these values into `$XDG_CONFIG_HOME/ldcli/config.yml`; leave them in the environment so they are never persisted to disk. (`LD_SERVER_URL` / `LD_PROJECT_NAME` may also be present as aliases but are not read by the CLI.)

**Running the Go unit tests.** A few tests assert unauthenticated behavior (`cmd/flags`, `cmd/setup`, `cmd/whoami`), so `go test ./...` / `make test` fail while `LD_ACCESS_TOKEN`/`LD_BASE_URI`/`LD_PROJECT` are set. Run the offline suite with those vars unset:

```bash
env -u LD_ACCESS_TOKEN -u LD_BASE_URI -u LD_PROJECT -u LD_SERVER_URL -u LD_PROJECT_NAME go test ./...
```
