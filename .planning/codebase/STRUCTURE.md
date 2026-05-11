# Codebase Structure

**Analysis Date:** 2026-05-11

## Directory Layout

```
ldcli/
├── main.go                        # Binary entry point
├── go.mod / go.sum                # Go module definition
├── Makefile                       # Build, test, generate, vendor targets
├── ld-openapi.json                # LaunchDarkly OpenAPI spec (source of truth for generated code)
├── tools.go                       # Go tool dependencies (mockgen, oapi-codegen)
├── cmd/                           # Cobra command layer
│   ├── root.go                    # Root command, Execute(), NewRootCommand()
│   ├── quickstart.go              # Quick-start command (Bubbletea TUI)
│   ├── templates.go               # Custom Cobra usage template helpers
│   ├── cmdtest.go                 # Shared test helpers for command tests
│   ├── analytics/                 # CmdRunEventProperties, agent-context detection
│   ├── cliflags/                  # Flag name constants and helper fns (flags.go)
│   ├── config/                    # config get/set/list commands
│   ├── dev_server/                # dev-server subcommand group
│   ├── flags/                     # toggle-on, toggle-off, archive (hand-written)
│   ├── login/                     # login command
│   ├── members/                   # members invite (hand-written)
│   ├── resources/                 # Generated resource_cmds.go + generator + template
│   ├── sdk_active/                # sdk-active subcommand
│   ├── signup/                    # signup command
│   ├── sourcemaps/                # sourcemaps upload command
│   └── validators/                # Cobra Args validator
├── internal/                      # Domain packages (no external import)
│   ├── analytics/                 # Tracker interface, async HTTP client, noop/log variants
│   ├── client/                    # Builds ldapi.APIClient (used by typed domain clients)
│   ├── config/                    # Config struct, YAML I/O, XDG path resolution
│   ├── environments/              # environments.Client interface + impl
│   ├── errors/                    # errors.Error type, HTTP status suggestions
│   ├── flags/                     # flags.Client interface + impl (typed LD API client)
│   ├── login/                     # Login flow helper
│   ├── members/                   # members.Client interface + impl
│   ├── output/                    # Output formatting: json/plaintext/markdown/table
│   ├── projects/                  # projects.Client interface + impl
│   ├── quickstart/                # Bubbletea TUI step models
│   ├── resources/                 # resources.Client generic HTTP interface + impl
│   ├── sdks/                      # SDK instructions data and helpers
│   └── dev_server/                # Local dev server subsystem
│       ├── dev_server.go          # Client interface, RunServer(), HTTP router setup
│       ├── adapters/              # context.Context injectors for ldapi.APIClient and SDK
│       ├── api/                   # oapi-codegen handlers for /dev/* management API
│       │   ├── api.yaml           # OpenAPI spec for dev server management API
│       │   ├── server.gen.go      # Generated from api.yaml via oapi-codegen
│       │   └── events/            # SSE event streaming routes
│       ├── db/                    # SQLite implementation of model.Store
│       ├── events_db/             # SQLite implementation of model.EventStore
│       ├── model/                 # Domain model: Project, Override, Store interface, observers
│       ├── sdk/                   # SDK-compatible HTTP endpoints (streaming, polling, eval)
│       └── ui/                    # React 18 + TypeScript + Vite SPA (embedded into binary)
│           ├── src/               # TypeScript source files
│           ├── dist/              # Compiled SPA (committed, embedded via go:embed)
│           ├── package.json
│           └── vite.config.ts
├── scripts/                       # Shell scripts (release, CI helpers)
└── .planning/                     # AI planning documents (not compiled)
    └── codebase/
```

## Directory Purposes

**`cmd/`:**
- Purpose: Cobra command definitions only — no business logic
- Contains: `New*Cmd()` constructors, flag registration, `RunE` handlers that delegate to `internal/`
- Key files: `cmd/root.go` (command tree assembly), `cmd/resources/resource_cmds.go` (generated, 11k lines)

**`cmd/resources/`:**
- Purpose: OpenAPI-to-Cobra code generation pipeline and runtime helpers
- Contains: `gen_resources.go` (generator, build tag `gen_resources`), `resource_cmds.tmpl` (Go template), `resource_cmds.go` (generated output, do not edit), `resources.go` (runtime helpers: `NewOperationCmd`, `NewResourceCmd`, `OperationCmd.makeRequest`)
- Key files: `cmd/resources/resources.go`, `cmd/resources/resource_cmds.go`

**`cmd/cliflags/`:**
- Purpose: Single source of truth for all CLI flag name constants and their descriptions
- Contains: `flags.go` — all `const` flag names, defaults, descriptions, `AllFlagsHelp()`, `GetOutputKind()`, `GetFields()`
- Key files: `cmd/cliflags/flags.go`

**`internal/resources/`:**
- Purpose: Generic HTTP client used by all generated and most hand-written commands
- Contains: `Client` interface with `MakeRequest` and `MakeUnauthenticatedRequest`; error normalization logic
- Key files: `internal/resources/client.go`

**`internal/output/`:**
- Purpose: All response formatting — JSON passthrough, plaintext key-value, table rendering, markdown
- Contains: `output.go` (OutputKind, Outputter interface), `resource_output.go` (CmdOutput dispatcher), `outputters.go` (SingularOutputter, MultipleOutputter), `table.go`, `markdown.go`, `plaintext_fns.go`
- Key files: `internal/output/resource_output.go`

**`internal/dev_server/`:**
- Purpose: Self-contained local dev server subsystem
- Contains: gorilla/mux router, context-injected middleware, SQLite store, SDK-compat endpoints, oapi-codegen management API, embedded React SPA
- Key files: `internal/dev_server/dev_server.go`, `internal/dev_server/model/store.go`, `internal/dev_server/sdk/routes.go`

**`internal/dev_server/ui/`:**
- Purpose: React SPA for flag override management; compiled output embedded in Go binary
- Contains: TypeScript/React source in `src/`, compiled output in `dist/` (committed to repo)
- Key files: `internal/dev_server/ui/asset_handler.go` (embed), `src/App.tsx`, `src/api.ts`
- Note: `dist/` is committed and must be rebuilt via `npm run build` when UI changes

## Key File Locations

**Entry Points:**
- `main.go`: Binary entry; sets version via ldflags
- `cmd/root.go`: `Execute()` (startup wiring), `NewRootCommand()` (command tree)

**Configuration:**
- `cmd/cliflags/flags.go`: All flag name constants
- `internal/config/config.go`: Config struct, YAML marshalling, `GetConfigFile()`
- `internal/config/config_service.go`: `VerifyAccessToken()` helper

**HTTP Clients:**
- `internal/resources/client.go`: Generic `Client` interface and `ResourcesClient` implementation
- `internal/client/client.go`: `client.New()` — builds `ldapi.APIClient` for typed domain clients
- `internal/flags/client.go`: Typed flags client example (pattern repeated in `environments/`, `members/`, `projects/`)

**Output:**
- `internal/output/resource_output.go`: `CmdOutput()` — main formatting dispatcher
- `internal/output/output.go`: `OutputKind` type, `Outputter` interface

**Analytics:**
- `internal/analytics/tracker.go`: `Tracker` interface and `TrackerFn` type
- `internal/analytics/client.go`: Async HTTP implementation, `ClientFn.Tracker()` factory
- `cmd/analytics/analytics.go`: `CmdRunEventProperties()`, `DetectAgentContext()`

**Code Generation:**
- `ld-openapi.json`: Source OpenAPI spec (~3MB, do not edit manually for code generation)
- `cmd/resources/gen_resources.go`: Generator (build tag `gen_resources`)
- `cmd/resources/resource_cmds.tmpl`: Go template
- `cmd/resources/resource_cmds.go`: Generated output (do not edit)

**Dev Server:**
- `internal/dev_server/dev_server.go`: `RunServer()` — main server setup
- `internal/dev_server/api/api.yaml`: Management API OpenAPI spec
- `internal/dev_server/api/server.gen.go`: Generated management API handlers
- `internal/dev_server/model/store.go`: `Store` interface
- `internal/dev_server/db/sqlite.go`: SQLite implementation
- `internal/dev_server/sdk/routes.go`: SDK-compatible endpoint bindings

**Testing:**
- `cmd/cmdtest.go`: Shared test helpers for command-layer tests
- `cmd/config/testdata/`: YAML config fixtures
- `cmd/resources/test_data/`: JSON response fixtures for resource command tests

## Naming Conventions

**Files:**
- Go source: `snake_case.go` (e.g., `resource_cmds.go`, `config_service.go`)
- Test files: `<file>_test.go` co-located with source
- Mock files: `mock_client.go` or `mock.go` co-located with the interface they mock
- Generated files: contain `// This file is generated` or `// Code generated` header comment

**Directories:**
- Go packages: `snake_case` (e.g., `dev_server`, `sdk_active`, `cliflags`)
- Each `cmd/` subdirectory is its own package named after the subcommand domain

**Functions:**
- Command constructors: `New<Name>Cmd(...)` returning `*cobra.Command` or a wrapper struct
- Client implementations: Named after the domain (e.g., `FlagsClient`, `ResourcesClient`)

**Types:**
- Interfaces: use the noun without suffix (e.g., `Client`, `Store`, `Tracker`)
- Concrete implementations: `<Domain>Client` (e.g., `FlagsClient`, `ResourcesClient`)
- Mock types: generated by mockgen; live in `mock_client.go` or `mocks/` subdirectory

## Where to Add New Code

**New resource command (from OpenAPI spec):**
- Update `ld-openapi.json` (or run `make openapi-spec-update`)
- Run `make generate` to regenerate `cmd/resources/resource_cmds.go`
- No manual code changes needed for standard CRUD operations

**New hand-written subcommand under an existing resource (e.g., new `flags` action):**
- Add `New<Action>Cmd()` in `cmd/flags/` (or appropriate `cmd/<resource>/` package)
- Register it in `cmd/root.go:NewRootCommand()` under the relevant resource block (lines 262–275)
- Tests: add `<action>_test.go` in the same package

**New top-level subcommand:**
- Create `cmd/<name>/` package with `New<Name>Cmd()` constructor
- Import and register via `cmd.AddCommand()` in `cmd/root.go:NewRootCommand()`
- Update `getUsageTemplate()` in `cmd/templates.go` if it needs to appear in root help
- Add analytics instrumentation via `PersistentPreRun` calling `tracker.SendCommandRunEvent`

**New internal domain client:**
- Create `internal/<domain>/` with a `Client` interface, concrete struct, and `NewClient(cliVersion string)` constructor
- Add a `mock_client.go` (or `mock.go`) using `//go:generate go run go.uber.org/mock/mockgen`
- Add the client to the `APIClients` struct in `cmd/root.go:40` and wire it in `Execute()`

**New dev server management API endpoint:**
- Add operation to `internal/dev_server/api/api.yaml`
- Run `go generate ./internal/dev_server/api/` to regenerate `server.gen.go`
- Implement the handler method on the `server` struct in a new `<operation>.go` file under `internal/dev_server/api/`

**New output formatter / column definition:**
- Add column definitions in `internal/output/plaintext_fns.go` (for plaintext key-value) or `internal/output/table.go` (for table output)
- Register via `GetSingularColumns()` / `GetListColumns()` if applicable

**Shared utility:**
- Flag helpers: `cmd/cliflags/flags.go`
- Shared test helpers: `cmd/cmdtest.go`

## Special Directories

**`cmd/resources/test_data/`:**
- Purpose: JSON response fixtures used by resource command tests
- Generated: No
- Committed: Yes

**`cmd/config/testdata/`:**
- Purpose: YAML config file fixtures for config command tests
- Generated: No
- Committed: Yes

**`internal/dev_server/ui/dist/`:**
- Purpose: Compiled React SPA; embedded into Go binary via `//go:embed all:dist` in `asset_handler.go`
- Generated: Yes (via `npm run build` in `internal/dev_server/ui/`)
- Committed: Yes (must be committed so the binary can be built without npm)

**`internal/dev_server/db/backup/`:**
- Purpose: SQLite backup/restore manager
- Generated: No
- Committed: Yes

**`.planning/codebase/`:**
- Purpose: AI codebase mapping documents (ARCHITECTURE.md, STACK.md, etc.)
- Generated: Yes (by GSD tooling)
- Committed: Yes

**`vendor/`** (if present after `make vendor`):
- Purpose: Vendored Go dependencies
- Generated: Yes
- Committed: Depends on repo policy (`.gitignore` not present for it, managed by `make vendor`)

---

*Structure analysis: 2026-05-11*
