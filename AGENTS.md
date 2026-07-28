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

Services and how to run/verify them (standard commands live in the sections above; only non-obvious caveats are noted here):

- **`ldcli` CLI** — `make build` produces `./ldcli`. Tests: `make test`. Lint: `golangci-lint run ./...` (see gotcha below). No credentials needed to build/test/lint.
- **Dev server (Go HTTP + SQLite + embedded React UI)** — `./ldcli dev-server start` serves the API and the embedded UI at `http://localhost:8765/ui/`.
  - `--access-token` is a required flag even to boot. The token is only used when syncing a project from LaunchDarkly, so any placeholder in the `api-<uuid>` token format (e.g. an all-zero UUID) is enough to start the server, serve the UI, and exercise the local HTTP API offline (e.g. `curl http://localhost:8765/dev/projects` → `[]`). Real flag data requires a real token.
  - SQLite DBs live at `~/.local/state/ldcli/dev_server.db` (and `_events.db`).
- **Dev server UI** — standard `npm` scripts in `internal/dev_server/ui` (`npm run lint` / `npm test` / `npm run build`). The production build (`dist/`) is checked into the repo and embedded into the Go binary.

Gotchas:

- **golangci-lint toolchain downgrade:** v1.63.4 refuses to run when the Go version it was *built with* is lower than the repo's `go.mod` target (currently 1.24.3), failing with `the Go language version (go1.22) used to build golangci-lint is lower than the targeted Go version`. Plain `go install .../golangci-lint@v1.63.4` (and the `make install-hooks` / pre-commit `golangci-lint` hook) auto-downgrade the toolchain to go1.22.2, so lint fails locally. Install it forcing the local toolchain: `GOTOOLCHAIN=$(go env GOVERSION) go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63.4`. CI avoids this only because it runs on a newer Go (stable). Ensure `$(go env GOPATH)/bin` (`~/go/bin`) is on `PATH`.
