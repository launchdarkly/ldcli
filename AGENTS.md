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

<!-- GSD:project-start source:PROJECT.md -->
## Project

**ldcli — Automated Rollouts via CLI**

ldcli is LaunchDarkly's official Go CLI for managing feature flags, environments, members, and a local dev server. This milestone adds a new `ldcli flags rollouts-beta` command surface for **starting, monitoring, and managing automated releases** (guarded + progressive rollouts) — designed so humans, CI/CD pipelines, and AI agents can safely ship features end-to-end behind a flag without leaving the terminal.

**Core Value:** An AI agent (or human, or CI/CD pipeline) can take a merged feature behind a flag, kick off an automated rollout, monitor it through to completion, and respond to regressions — without ever needing the LaunchDarkly UI.

### Constraints

- **Tech stack**: Must integrate with existing ldcli architecture — Cobra subcommands, the `internal/` `Client` interface pattern for testability, JSON/plaintext output formatting, and the OpenAPI-driven resource command generator where applicable.
- **API stability**: The `automated-releases` API is unstable. Work around issues as they come up; document papercuts; don't block on upstream API fixes.
- **Beta surface**: The `-beta` suffix carries forward; breaking changes are acceptable within this command tree.
- **Backwards compatibility**: Must not break any existing ldcli command, distribution channel, or analytics behavior.
- **Authentication**: Reuse existing ldcli auth (OAuth + access tokens via `ldcli config`); no new auth surface.
<!-- GSD:project-end -->

<!-- GSD:stack-start source:codebase/STACK.md -->
## Technology Stack

## Languages
- Go 1.23.0 - All backend CLI logic, dev server HTTP service, code generation pipeline
- TypeScript 5.2.2 - Dev server embedded React frontend (`internal/dev_server/ui/src/`)
- Go templates (`text/template`) - OpenAPI resource command generation (`cmd/resources/resource_cmds.tmpl`)
## Runtime
- Go toolchain (CGO enabled; required for SQLite via `github.com/mattn/go-sqlite3`)
- Node.js LTS (CI uses `lts/*`; npm publish workflows use 20.x) - frontend build only
- Go modules (`go.mod` / `go.sum`) — vendor directory not present by default; `make vendor` creates it
- npm (root `package.json` wraps binary distribution via `@go-task/go-npm`)
- npm (inner `internal/dev_server/ui/package.json`) — dev server frontend
- `go.sum` — present
- `package-lock.json` — present at repo root and in `internal/dev_server/ui/`
## Frameworks
- `github.com/spf13/cobra` v1.9.1 - Command/subcommand routing, help text, flag binding
- `github.com/spf13/viper` v1.21.0 - Config file + env var + flag precedence chain
- `github.com/spf13/pflag` v1.0.10 - POSIX-style flag parsing (used by Cobra)
- `github.com/gorilla/mux` v1.8.1 - Router for dev server (`internal/dev_server/dev_server.go`)
- `github.com/gorilla/handlers` v1.5.2 - CORS, recovery, logging middleware
- `github.com/oapi-codegen/oapi-codegen/v2` v2.4.1 - Generates `internal/dev_server/api/server.gen.go` from `internal/dev_server/api/api.yaml`
- `github.com/oapi-codegen/runtime` v1.1.2 - Runtime support for generated server code
- `github.com/charmbracelet/bubbletea` v1.3.6 - Elm-style TUI framework (`internal/quickstart/`, `cmd/quickstart.go`)
- `github.com/charmbracelet/bubbles` v0.21.0 - TUI components (list, help, key bindings)
- `github.com/charmbracelet/lipgloss` v1.1.1-0.20250404203927 - Terminal styling
- `github.com/charmbracelet/glamour` v0.10.0 - Markdown rendering in terminal (`cmd/resources/resources.go`, `cmd/resources/resource_cmds.go`)
- React 18.3.1 - UI framework (`internal/dev_server/ui/src/`)
- React Router 7.12.0 - SPA routing
- Vite 6.4.1 - Build tool; `vite-plugin-singlefile` bundles into one HTML file embedded in Go binary
- TypeScript 5.2.2
- `github.com/getkin/kin-openapi` v0.127.0 - Parses `ld-openapi.json` to drive template generation (`cmd/resources/resources.go`)
- `github.com/iancoleman/strcase` v0.3.0 - Case conversion during codegen
- Go `text/template` + `go/format` - Template execution and source formatting (`cmd/resources/gen_resources.go`)
- `github.com/stretchr/testify` v1.11.1 - Assertions and test helpers
- `go.uber.org/mock` v0.5.2 - Mock generation via `mockgen`; mocks in `*/mocks/` packages
- Vitest v2.1.9 - Frontend unit test runner
- `@testing-library/react` v16.0.1 - React component testing
- GoReleaser v2 (`.goreleaser.yaml`) - Cross-compilation and release artifact building
- goreleaser-cross Docker image - Provides cross-compiler toolchains (musl, mingw, osxcross) for CGO cross-compilation
- `release-please` (googleapis/release-please-action v4.4.0) - Automated changelog and GitHub release management
- pre-commit (`.pre-commit-config.yaml`) - Git hook runner for `go fmt`, `go mod tidy`, frontend tests/build
## Key Dependencies
- `github.com/launchdarkly/api-client-go/v14` v14.0.0 - Auto-generated Go client for the LaunchDarkly REST API; used by `internal/client/client.go` for all resource commands
- `github.com/launchdarkly/go-server-sdk/v7` v7.13.4 - Go server-side LD SDK; used by dev server to stream all flag state (`internal/dev_server/adapters/sdk.go`)
- `github.com/launchdarkly/go-sdk-common/v3` v3.4.0 - Shared LD SDK types (ldcontext, ldvalue)
- `github.com/launchdarkly/sdk-meta/api` v0.4.8 - SDK metadata (names, instructions) for quickstart (`internal/quickstart/`)
- `github.com/mattn/go-sqlite3` v1.14.28 - SQLite driver (CGO); required for dev server local storage (`internal/dev_server/db/`)
- `github.com/adrg/xdg` v0.5.3 - XDG Base Directory spec; resolves config and state paths (`internal/config/config.go`, `internal/dev_server/dev_server.go`)
- `github.com/mitchellh/go-homedir` v1.1.0 - Fallback home directory detection
- `github.com/google/uuid` v1.6.0 - UUID generation for analytics client ID
- `github.com/pkg/browser` v0.0.0-20240102092130 - Opens browser for login/signup flows
- `github.com/pkg/errors` v0.9.1 - Structured error wrapping
- `github.com/samber/lo` v1.51.0 - Generic functional helpers
- `golang.org/x/term` v0.33.0 - Terminal detection (e.g., TTY check for output formatting)
- `gopkg.in/yaml.v3` v3.0.1 - Config file serialization (`internal/config/config.go`)
- `@launchpad-ui/components` 0.4.4, `@launchpad-ui/core` 0.49.22, `@launchpad-ui/icons` 0.18.13, `@launchpad-ui/tokens` 0.11.3 - LaunchDarkly's internal design system
- `launchdarkly-js-client-sdk` 3.4.0 - Type definitions for flag values used in the dev server UI
- `fuzzysort` 3.0.2 - Fuzzy search for flag list
- `react-window` 1.8.10 - Virtualized list rendering for large flag lists
- `lodash` 4.17.23 - Utility functions
## Configuration
- Path: `$XDG_CONFIG_HOME/ldcli/config.yml` (default: `~/.config/ldcli/config.yml`)
- Managed by: `internal/config/config.go`
- Fields: `access-token`, `analytics-opt-out`, `base-uri`, `dev-stream-uri`, `environment`, `flag`, `output`, `project`
- `BaseURIDefault = "https://app.launchdarkly.com"`
- `DevStreamURIDefault = "https://stream.launchdarkly.com"`
- `PortDefault = "8765"` (dev server local port)
- `.goreleaser.yaml` — cross-compilation targets: linux (amd64, arm64, 386), darwin (amd64, arm64), windows (amd64, arm64, 386)
- `CGO_ENABLED=1` required in all builds (SQLite)
- `-X 'main.version={{.Version}}'` injects version at link time
## Platform Requirements
- Go 1.23+
- CGO-capable C compiler (gcc/clang) for SQLite
- Node.js LTS + npm (for dev server frontend work)
- `make install-hooks` to enable pre-commit checks
- Single static binary (Linux: statically linked via musl; macOS/Windows: dynamically linked but self-contained)
- No external runtime dependencies; SQLite and frontend assets are embedded
- Dev server SQLite databases stored in XDG state directory: `$XDG_STATE_HOME/ldcli/dev_server.db` and `$XDG_STATE_HOME/ldcli/dev_server_events.db`
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

## Naming Patterns
- Go source: `snake_case.go` (e.g., `mock_client.go`, `config_service.go`)
- Go test files: `<source_file>_test.go` co-located with source (e.g., `client_test.go` alongside `client.go`)
- Generated Go files: prefixed with `// This file is generated by ...; DO NOT EDIT.` (e.g., `cmd/resources/resource_cmds.go`)
- MockGen-generated files: prefixed with `// Code generated by MockGen. DO NOT EDIT.` (e.g., `internal/dev_server/model/mocks/store.go`)
- Frontend: `PascalCase.tsx` for React components (e.g., `FlagsPage.tsx`, `SubmitButton.tsx`), `camelCase.ts` for utilities (e.g., `api.ts`, `util.ts`, `types.ts`)
- Match directory name: `package flags`, `package resources`, `package analytics`
- Command packages use short aliases on import: `flagscmd`, `configcmd`, `devcmd`, `memberscmd`
- Test files use external test packages: `package flags_test`, `package model_test`, `package resources_test`
- Exception: a few test files use the same package for white-box access (e.g., `internal/output/plaintext_fns_internal_test.go` uses `package output`)
- Exported: `PascalCase` (e.g., `NewClient`, `MakeRequest`, `CallCmd`)
- Unexported: `camelCase` (e.g., `runE`, `buildPatch`, `initFlags`, `makeServer`)
- Constructor pattern: `New<Type>` returns a concrete type or interface (e.g., `NewClient`, `NewRootCommand`, `NewToggleOnCmd`)
- Cobra command constructors: `New<Name>Cmd` returns `*cobra.Command` or a struct wrapping it (e.g., `NewToggleOnCmd`, `NewConfigCmd`)
- Command runner closures: `runE(client)` returns `func(*cobra.Command, []string) error`
- Flag name constants: `SCREAMING_SNAKE_CASE` with `Flag` suffix (e.g., `AccessTokenFlag = "access-token"`, `BaseURIFlag = "base-uri"`)
- Flag descriptions: `<FlagName>Description` constant paired with the flag constant
- Context key types: unexported typed `string` alias to prevent collisions (e.g., `type ctxKey string; const ctxKeyStore = ctxKey("model.Store")`)
- Interfaces: noun-based without `I` prefix (e.g., `Client`, `Store`, `Tracker`, `Sdk`)
- Concrete implementations: `<Domain>Client` (e.g., `FlagsClient`, `ResourcesClient`, `EnvironmentsClient`)
- Mock implementations: `MockClient` or `Mock<InterfaceName>` in the same package as the interface
## Interface Compliance Pattern
## Code Style
- `go fmt` enforced via pre-commit hook (`git/hooks/` installed via `make install-hooks`)
- golangci-lint v1.63.4 via `.pre-commit-config.yaml`
- Config: `.pre-commit-config.yaml` (golangci-lint `v1.63.4`)
- Hook: `golangci-lint run`, runs on all `.go` files, `require_serial: true`
- Additional hook: `end-of-file-fixer` (all files must end with newline)
- ESLint v8 with TypeScript + React plugins (`eslint.config.js`)
- Rules: `react/react-in-jsx-scope: off` (React 18 JSX transform)
- Prettier: `singleQuote: true`, `trailingComma: all`, `tabWidth: 2`, `semi: true` (`.prettierrc`)
- TypeScript strict mode (`tsconfig.json`)
- Run: `npm run lint` from `internal/dev_server/ui/`
## Import Organization
- Command subpackages imported with short aliases: `flagscmd`, `logincmd`, `configcmd`, `devcmd`, `memberscmd`, `sdkactivecmd`, `resourcecmd`, `signupcmd`, `sourcemapscmd`
- Internal errors package: `errs "github.com/launchdarkly/ldcli/internal/errors"`
- `cmd/analytics` package: `cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"`
- External SDK package: `ldsdk "github.com/launchdarkly/go-server-sdk/v7"`
## Error Handling
- `internal/errors/errors.go` defines the central error system
- `errors.Error` — a typed error wrapping `pkg/errors` with a user-facing message
- `errors.NewError(message string) error` — creates a stack-traced `errors.Error`
- `errors.NewErrorWrapped(message, underlying error) error` — wraps an underlying error
- `errors.NewLDAPIError(err error) error` — normalizes errors from the LD API client
- `errors.APIError` — carries body bytes + model for API responses
- Always return `error` as the last return value
- Use `require.NoError(t, err)` in tests before asserting on results
- Check `errors.As(err, &target)` for typed error inspection (not `errors.Is` for wrapping)
- Cobra `RunE` functions return errors directly; the root command handles display
- API client errors are converted at the boundary: `errors.NewLDAPIError(err)`
- Output errors use: `output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))`
## Dependency Injection Pattern
## Cobra Command Patterns
- Analytics events sent in `PreRun` or `PersistentPreRun` hooks
- `cmd/analytics/analytics.go` provides `CmdRunEventProperties` helper
- `analytics.TrackerFn` (a `func(accessToken, baseURI string, optOut bool) Tracker`) is injected into commands that track events
- Commands that don't need tracking receive `analytics.NoopClientFn{}.Tracker()`
## Context Pattern (dev_server)
## Logging
- Used sparingly in the dev server (`internal/dev_server/adapters/sdk.go`)
- Analytics errors are silent (commented `TODO: log error` in `internal/analytics/client.go:64,75,87,96`)
- No log levels or structured fields in the main CLI path
## Comments
- Complex logic or non-obvious decisions
- Public function/method signatures: sparse godoc, only when truly useful
- Generated files: always have `// Code generated by ...; DO NOT EDIT.` at top
- TODO/FIXME comments are present (`cmd/resources/resources.go:143`, `internal/analytics/client.go:64,75`)
## Module Design
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

## System Overview
```text
```
## Component Responsibilities
| Component | Responsibility | File |
|-----------|----------------|------|
| `main.go` | Binary entry point; sets version via ldflags | `main.go` |
| `cmd/root.go` | Wires CLI tree: creates all clients, registers subcommands, config, analytics | `cmd/root.go` |
| `cmd/resources/` | Generated + utility code for OpenAPI-driven resource commands | `cmd/resources/resource_cmds.go`, `resources.go` |
| `cmd/flags/` | Hand-written toggle-on/off and archive commands for flags resource | `cmd/flags/toggle.go`, `archive.go` |
| `cmd/dev_server/` | dev-server subcommand group: start, projects, overrides, import | `cmd/dev_server/dev_server.go` |
| `cmd/config/` | config get/set/list commands | `cmd/config/config.go` |
| `cmd/analytics/` | Analytics event helpers and agent-context detection | `cmd/analytics/analytics.go` |
| `cmd/cliflags/` | Shared flag name constants and helper functions | `cmd/cliflags/flags.go` |
| `internal/resources/` | Core HTTP client: `MakeRequest`, error normalization | `internal/resources/client.go` |
| `internal/flags/` | Typed flags client using `launchdarkly/api-client-go` | `internal/flags/client.go` |
| `internal/config/` | Config struct, YAML read/write, `$XDG_CONFIG_HOME` path resolution | `internal/config/config.go` |
| `internal/output/` | Output formatting (json/plaintext/markdown), table rendering | `internal/output/resource_output.go` |
| `internal/analytics/` | Async analytics tracker, noop/log variants | `internal/analytics/client.go` |
| `internal/client/` | Creates `ldapi.APIClient` (used by typed clients in flags/environments) | `internal/client/client.go` |
| `internal/dev_server/` | Local dev server: HTTP server, SQLite, SDK integration, embedded UI | `internal/dev_server/dev_server.go` |
| `internal/dev_server/model/` | Domain model: Project, Override, FlagsState, Store interface | `internal/dev_server/model/` |
| `internal/dev_server/db/` | SQLite implementation of `model.Store` | `internal/dev_server/db/sqlite.go` |
| `internal/dev_server/api/` | oapi-codegen generated HTTP handlers for `/dev/*` routes | `internal/dev_server/api/server.gen.go` |
| `internal/dev_server/sdk/` | SDK-compatible endpoints (streaming, polling, eval, FDv2) | `internal/dev_server/sdk/routes.go` |
| `internal/dev_server/adapters/` | Puts `ldapi.APIClient` and SDK adapter on `context.Context` via middleware | `internal/dev_server/adapters/` |
| `internal/dev_server/ui/` | React 18/TypeScript/Vite SPA; compiled to `dist/` and embedded in binary | `internal/dev_server/ui/asset_handler.go` |
| `internal/quickstart/` | Interactive Bubbletea TUI flow (create flag → choose SDK → toggle) | `internal/quickstart/container.go` |
## Pattern Overview
- All domain packages expose a `Client` interface; concrete implementations are injected at startup in `cmd/root.go`'s `Execute()`
- Configuration precedence: CLI flags → `LD_*` env vars → `$XDG_CONFIG_HOME/ldcli/config.yml`
- Two HTTP client strategies: typed `ldapi.APIClient` (used by `internal/flags/`, `internal/environments/`) and the generic `resources.Client` (used by generated and most hand-written commands)
- Resource commands for the full LD API surface are code-generated from `ld-openapi.json` via a Go template pipeline
- The dev server is a self-contained embedded subsystem with its own router, SQLite store, SDK compatibility layer, and React SPA
## Layers
- Purpose: Defines Cobra commands, parses flags, calls internal clients, formats output
- Location: `cmd/`
- Contains: Cobra command constructors (`New*Cmd`), flag registration, `RunE` handlers
- Depends on: `internal/resources/`, `internal/output/`, `internal/analytics/`, `cmd/cliflags/`, `cmd/validators/`
- Used by: `cmd/root.go` (adds all subcommands)
- Purpose: Typed interfaces and implementations for interacting with the LD API
- Location: `internal/flags/`, `internal/environments/`, `internal/members/`, `internal/projects/`, `internal/resources/`
- Contains: `Client` interfaces, concrete structs, mock implementations (`mock_client.go`, `mock.go`)
- Depends on: `internal/client/` (for `ldapi.APIClient`), `internal/errors/`
- Used by: `cmd/` layer
- `internal/config/` — YAML config file management; `GetConfigFile()` returns XDG path
- `internal/output/` — `CmdOutput()` dispatch; `Outputter` interface; table/kv/markdown renderers
- `internal/analytics/` — `Tracker` interface; async HTTP sends to `{baseURI}/internal/tracking`; `ClientFn.Tracker()` factory
- `internal/errors/` — `errors.Error` type; `SuggestionForStatus()` adds human-readable hints
- `internal/client/` — `client.New()` builds an `ldapi.APIClient` with auth headers
- Purpose: Local HTTP server that proxies LD SDK calls and allows flag overrides
- Location: `internal/dev_server/`
- Contains: gorilla/mux router, oapi-codegen handlers, SQLite store, SDK-compatible endpoints, embedded React SPA
- Depends on: `internal/client/`, `launchdarkly/go-server-sdk/v7`, `launchdarkly/api-client-go/v14`, `mattn/go-sqlite3`
- Used by: `cmd/dev_server/`
- Purpose: React SPA for managing flag overrides interactively
- Location: `internal/dev_server/ui/src/`
- Contains: React components, TypeScript types, Vite config; compiled output in `dist/` is embedded at build time via `//go:embed all:dist`
- Depends on: `@launchpad-ui/*`, `react-router`, `launchdarkly-js-client-sdk`
## Data Flow
### CLI Command Execution
### Generated Resource Command Path
### Dev Server Request Path
### Configuration Resolution
## Key Abstractions
- Purpose: Single interface for all generated + most hand-written commands; handles auth headers, beta flag, error normalization
- Examples: `internal/resources/client.go`
- Pattern: All callers read token/baseURI from Viper at call time, not at construction time
- Purpose: Per-domain interfaces enabling mock injection in tests
- Examples: `internal/flags/client.go:Client`, `internal/environments/client.go:Client`, `internal/members/members.go`, `internal/projects/projects.go`
- Pattern: `var _ Client = ConcreteImpl{}` compile-time assertion; mocks generated via `go.uber.org/mock/mockgen`
- Purpose: Abstraction over SQLite for the dev server; all model operations use context-injected store
- Examples: `internal/dev_server/model/store.go:Store`
- Pattern: `model.StoreMiddleware` injects into context; handlers call `model.StoreFromContext(ctx)`
- Purpose: Allows noop/log/real implementations; created per-invocation via `TrackerFn`
- Examples: `internal/analytics/tracker.go`, `noop_client.go`, `log_client.go`
- Pattern: `ClientFn.Tracker(token, baseURI, optOut)` factory returns `NoopClient` if opted out
- Purpose: Decouples JSON vs plaintext formatting from command handlers
- Examples: `internal/output/output.go`, `outputters.go`
- Pattern: `CmdOutput(action, outputKind, bytes)` dispatches to singular/multiple outputters
## Entry Points
- Location: `main.go`
- Triggers: User running `ldcli` binary
- Responsibilities: Sets `version` from ldflags, calls `cmd.Execute()`
- Location: `cmd/root.go:282`
- Triggers: Called by `main()`
- Responsibilities: Constructs all concrete clients, config service, analytics; builds root command tree; handles exit codes and analytics drain
- Location: `cmd/root.go:109`
- Triggers: Called by `Execute()`
- Responsibilities: Registers all persistent flags, reads config, adds all subcommands, wires analytics to help handler
- Location: `internal/dev_server/dev_server.go:49`
- Triggers: `ldcli dev-server start`
- Responsibilities: Opens SQLite, builds gorilla/mux router with all middleware, registers SDK + management + UI routes, starts HTTP server
- Location: `cmd/resources/resource_cmds.go` (generated)
- Triggers: Called from `NewRootCommand()`
- Responsibilities: Registers every LD API resource and operation as a Cobra subcommand
## Architectural Constraints
- **Threading:** Single-threaded Cobra command execution; analytics HTTP calls are fire-and-forget goroutines drained by `analyticsClient.Wait()` before process exit
- **Global state:** Viper global instance used throughout — flag values are read via `viper.GetString()` at `RunE` time, not at command construction time; `cobra.AddTemplateFunc` called in `cmd/root.go:init()`
- **Circular imports:** `cmd/` packages import `internal/` packages; `internal/` packages must not import `cmd/`; `internal/config/` imports `cmd/cliflags/` (flag name constants only)
- **Code generation boundary:** `cmd/resources/resource_cmds.go` (11,111 lines) is never edited manually; all changes go through the OpenAPI spec → template pipeline
## Anti-Patterns
### Reading Viper inside command constructors
### Bypassing the `resources.Client` interface in hand-written commands
## Error Handling
- HTTP errors ≥ 400 are normalized into a JSON map `{code, message, statusCode, suggestion}` in `internal/resources/client.go:82–110`
- `errors.SuggestionForStatus()` appends human-readable hints (e.g., "check your access token") for common HTTP status codes
- `SilenceErrors: true` and `SilenceUsage: true` on root command prevent double-printing; error is written to stderr manually in `Execute()` (`cmd/root.go:329`)
- LD API typed client errors go through `errors.NewLDAPIError()` before surfacing
## Cross-Cutting Concerns
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
