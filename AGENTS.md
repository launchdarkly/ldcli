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
make vendor             # go mod tidy + go mod vendor (vendor/ is not committed; usually you only want go mod tidy)
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
- `LD_*` environment variables and `~/.config/ldcli/config.yml` leak into `cmd/` tests and make some of them fail (e.g. `cmd/setup`, `cmd/whoami`). Run tests with `LD_*` unset and `XDG_CONFIG_HOME` pointed at an empty directory.

## Pre-commit Hooks

Installed via `make install-hooks`. `.pre-commit-config.yaml` runs only:
- `golangci-lint` v1.63.4 (default linters, no `.golangci.yml`)
- `end-of-file-fixer`

It does **not** check `go fmt`, `go mod tidy`, or the UI build. Run `go mod tidy` yourself; `scripts/dependency-pr/verify.sh` checks tidiness, codegen drift, and UI `dist/` drift.

## Dependency PR Verification

To verify a Dependabot PR, follow the `verify-dependency-pr` skill (`.agents/skills/verify-dependency-pr/SKILL.md`). Entry point: `scripts/dependency-pr/verify.sh --pr <N>` (or `make verify-dependency-pr ARGS="--pr <N>"`). It writes `.verify-out/pr-<N>/{result.json,comment.md}` and never posts anything. `post-comment.sh` is the only script that comments, and the agent never approves or merges. Unit tests: `scripts/dependency-pr/test/run.sh`.

## Linting

- Go: `golangci-lint` (v1.63.4) via pre-commit
- Frontend: ESLint + Prettier
