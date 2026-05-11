<!-- refreshed: 2026-05-11 -->
# Architecture

**Analysis Date:** 2026-05-11

## System Overview

```text
┌─────────────────────────────────────────────────────────────────────────┐
│                           main.go                                        │
│                    cmd.Execute(version)                                  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       cmd/root.go  (Cobra root)                          │
│   NewRootCommand() wires all clients, config, analytics, subcommands    │
└──────┬────────┬────────┬────────┬────────┬────────┬──────────┬──────────┘
       │        │        │        │        │        │          │
       ▼        ▼        ▼        ▼        ▼        ▼          ▼
  cmd/config cmd/login cmd/flags cmd/members cmd/dev_server cmd/sourcemaps
  cmd/signup cmd/resources (generated) cmd/sdk_active  cmd/quickstart.go
└──────────────────────────────┬──────────────────────────────────────────┘
                               │ each cmd calls into internal/ packages
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          internal/                                       │
│  resources/   flags/   environments/  members/   projects/              │
│  config/      output/  analytics/     errors/    client/                │
│  dev_server/  login/   sdks/          quickstart/                       │
└──────────────────────────────┬──────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  LaunchDarkly REST API  /  local SQLite DB  /  embedded React UI         │
└─────────────────────────────────────────────────────────────────────────┘
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

**Overall:** Cobra/Viper CLI with a thin command layer over domain clients with dependency injection.

**Key Characteristics:**
- All domain packages expose a `Client` interface; concrete implementations are injected at startup in `cmd/root.go`'s `Execute()`
- Configuration precedence: CLI flags → `LD_*` env vars → `$XDG_CONFIG_HOME/ldcli/config.yml`
- Two HTTP client strategies: typed `ldapi.APIClient` (used by `internal/flags/`, `internal/environments/`) and the generic `resources.Client` (used by generated and most hand-written commands)
- Resource commands for the full LD API surface are code-generated from `ld-openapi.json` via a Go template pipeline
- The dev server is a self-contained embedded subsystem with its own router, SQLite store, SDK compatibility layer, and React SPA

## Layers

**Command Layer (`cmd/`):**
- Purpose: Defines Cobra commands, parses flags, calls internal clients, formats output
- Location: `cmd/`
- Contains: Cobra command constructors (`New*Cmd`), flag registration, `RunE` handlers
- Depends on: `internal/resources/`, `internal/output/`, `internal/analytics/`, `cmd/cliflags/`, `cmd/validators/`
- Used by: `cmd/root.go` (adds all subcommands)

**Domain Client Layer (`internal/<domain>/`):**
- Purpose: Typed interfaces and implementations for interacting with the LD API
- Location: `internal/flags/`, `internal/environments/`, `internal/members/`, `internal/projects/`, `internal/resources/`
- Contains: `Client` interfaces, concrete structs, mock implementations (`mock_client.go`, `mock.go`)
- Depends on: `internal/client/` (for `ldapi.APIClient`), `internal/errors/`
- Used by: `cmd/` layer

**Infrastructure Layer (`internal/`):**
- `internal/config/` — YAML config file management; `GetConfigFile()` returns XDG path
- `internal/output/` — `CmdOutput()` dispatch; `Outputter` interface; table/kv/markdown renderers
- `internal/analytics/` — `Tracker` interface; async HTTP sends to `{baseURI}/internal/tracking`; `ClientFn.Tracker()` factory
- `internal/errors/` — `errors.Error` type; `SuggestionForStatus()` adds human-readable hints
- `internal/client/` — `client.New()` builds an `ldapi.APIClient` with auth headers

**Dev Server Subsystem (`internal/dev_server/`):**
- Purpose: Local HTTP server that proxies LD SDK calls and allows flag overrides
- Location: `internal/dev_server/`
- Contains: gorilla/mux router, oapi-codegen handlers, SQLite store, SDK-compatible endpoints, embedded React SPA
- Depends on: `internal/client/`, `launchdarkly/go-server-sdk/v7`, `launchdarkly/api-client-go/v14`, `mattn/go-sqlite3`
- Used by: `cmd/dev_server/`

**Frontend (`internal/dev_server/ui/`):**
- Purpose: React SPA for managing flag overrides interactively
- Location: `internal/dev_server/ui/src/`
- Contains: React components, TypeScript types, Vite config; compiled output in `dist/` is embedded at build time via `//go:embed all:dist`
- Depends on: `@launchpad-ui/*`, `react-router`, `launchdarkly-js-client-sdk`

## Data Flow

### CLI Command Execution

1. User invokes binary → `main.go:main()` (`main.go:13`)
2. `cmd.Execute(version)` constructs all API clients, config service, analytics tracker (`cmd/root.go:282`)
3. `NewRootCommand()` registers subcommands, binds Viper to persistent flags, reads config file (`cmd/root.go:109`)
4. Cobra parses args; `PersistentPreRun` fires analytics `SendCommandRunEvent` on the resource parent command (`cmd/resources/resources.go:214`)
5. `RunE` handler reads flag values from Viper, calls domain client (`internal/resources/client.go` or typed client)
6. Response bytes passed through `output.CmdOutput()` → formatted string written to stdout (`internal/output/resource_output.go:26`)
7. After `Execute()` returns, analytics `SendCommandCompletedEvent` is fired and `Wait()` drains async goroutines (`cmd/root.go:336–351`)

### Generated Resource Command Path

1. `cmd/resources/resource_cmds.go:AddAllResourceCmds()` (generated) registers every LD API operation
2. Each `OperationCmd.makeRequest()` in `cmd/resources/resources.go:298` builds URL from path template, sets query params and body, calls `resources.Client.MakeRequest()`
3. Response normalized in `internal/resources/client.go`; errors get `statusCode` and `suggestion` fields appended

### Dev Server Request Path

1. `ldcli dev-server start` → `cmd/dev_server/start_server.go` → `dev_server.LDClient.RunServer()` (`internal/dev_server/dev_server.go:49`)
2. `adapters.Middleware` injects `ldapi.APIClient` and SDK adapter onto every request context
3. `model.StoreMiddleware` and `model.ObserversMiddleware` inject SQLite store and event observers
4. SDK-compatible routes (`/all`, `/sdk/flags`, `/eval/`, `/meval`, etc.) served by `internal/dev_server/sdk/`
5. Management API routes under `/dev/` served by oapi-codegen `server.gen.go` handlers
6. React SPA served at `/ui/` from embedded `dist/` via `ui.AssetHandler`

### Configuration Resolution

1. Viper reads `$XDG_CONFIG_HOME/ldcli/config.yml` (or `~/.config/ldcli/config.yml`) at startup (`cmd/root.go:176`)
2. `viper.SetEnvPrefix("LD")` + `AutomaticEnv()` maps `LD_ACCESS_TOKEN` → `access-token`, etc. (`cmd/root.go:183`)
3. Explicit `--flag` values override both

## Key Abstractions

**`resources.Client` (generic HTTP client):**
- Purpose: Single interface for all generated + most hand-written commands; handles auth headers, beta flag, error normalization
- Examples: `internal/resources/client.go`
- Pattern: All callers read token/baseURI from Viper at call time, not at construction time

**Domain `Client` interfaces (typed):**
- Purpose: Per-domain interfaces enabling mock injection in tests
- Examples: `internal/flags/client.go:Client`, `internal/environments/client.go:Client`, `internal/members/members.go`, `internal/projects/projects.go`
- Pattern: `var _ Client = ConcreteImpl{}` compile-time assertion; mocks generated via `go.uber.org/mock/mockgen`

**`model.Store` interface:**
- Purpose: Abstraction over SQLite for the dev server; all model operations use context-injected store
- Examples: `internal/dev_server/model/store.go:Store`
- Pattern: `model.StoreMiddleware` injects into context; handlers call `model.StoreFromContext(ctx)`

**`analytics.Tracker` interface:**
- Purpose: Allows noop/log/real implementations; created per-invocation via `TrackerFn`
- Examples: `internal/analytics/tracker.go`, `noop_client.go`, `log_client.go`
- Pattern: `ClientFn.Tracker(token, baseURI, optOut)` factory returns `NoopClient` if opted out

**`output.Outputter` interface:**
- Purpose: Decouples JSON vs plaintext formatting from command handlers
- Examples: `internal/output/output.go`, `outputters.go`
- Pattern: `CmdOutput(action, outputKind, bytes)` dispatches to singular/multiple outputters

## Entry Points

**CLI Binary:**
- Location: `main.go`
- Triggers: User running `ldcli` binary
- Responsibilities: Sets `version` from ldflags, calls `cmd.Execute()`

**`cmd.Execute()`:**
- Location: `cmd/root.go:282`
- Triggers: Called by `main()`
- Responsibilities: Constructs all concrete clients, config service, analytics; builds root command tree; handles exit codes and analytics drain

**`NewRootCommand()`:**
- Location: `cmd/root.go:109`
- Triggers: Called by `Execute()`
- Responsibilities: Registers all persistent flags, reads config, adds all subcommands, wires analytics to help handler

**`dev_server.LDClient.RunServer()`:**
- Location: `internal/dev_server/dev_server.go:49`
- Triggers: `ldcli dev-server start`
- Responsibilities: Opens SQLite, builds gorilla/mux router with all middleware, registers SDK + management + UI routes, starts HTTP server

**`resources.AddAllResourceCmds()`:**
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

**What happens:** Flag values like `viper.GetString(cliflags.AccessTokenFlag)` are sometimes read before `RunE` fires.
**Why it's wrong:** Viper isn't fully populated (env vars + config file) until after `NewRootCommand()` completes; early reads return empty strings.
**Do this instead:** Read all Viper values inside `RunE` or `makeRequest`, as done in `cmd/resources/resources.go:298` and `cmd/flags/toggle.go:57`.

### Bypassing the `resources.Client` interface in hand-written commands

**What happens:** Some older or specialized commands directly construct URLs and call `client.MakeRequest()` with raw strings rather than using helper utilities.
**Why it's wrong:** Skips URL normalization and error enrichment logic in `internal/resources/client.go`.
**Do this instead:** Use `buildURLWithParams()` from `cmd/resources/resources.go:285` and let `MakeRequest` handle error shaping.

## Error Handling

**Strategy:** Errors are returned up the call stack as `errors.Error` (a string-typed error); at the command boundary `output.NewCmdOutputError(err, outputKind)` converts to formatted output before returning from `RunE`.

**Patterns:**
- HTTP errors ≥ 400 are normalized into a JSON map `{code, message, statusCode, suggestion}` in `internal/resources/client.go:82–110`
- `errors.SuggestionForStatus()` appends human-readable hints (e.g., "check your access token") for common HTTP status codes
- `SilenceErrors: true` and `SilenceUsage: true` on root command prevent double-printing; error is written to stderr manually in `Execute()` (`cmd/root.go:329`)
- LD API typed client errors go through `errors.NewLDAPIError()` before surfacing

## Cross-Cutting Concerns

**Logging:** `log.Printf` / `log.Fatal` used in dev server subsystem (`internal/dev_server/`); CLI commands write to `cmd.OutOrStdout()` for testability
**Validation:** `cmd/validators/validators.go` provides a `Validate()` cobra `Args` function; flag-level validation relies on Cobra's `MarkFlagRequired` + `MarkPersistentFlagRequired`
**Authentication:** `--access-token` (or `LD_ACCESS_TOKEN` or config file) is a required persistent flag; injected as `Authorization` header in `internal/resources/client.go:53` and `internal/client/client.go:14`
**Output format:** Controlled by `--output` (json/plaintext/markdown) or `--json` shorthand; defaults to `json` when stdout is not a TTY; overridable via `FORCE_TTY` / `LD_FORCE_TTY` env vars

---

*Architecture analysis: 2026-05-11*
