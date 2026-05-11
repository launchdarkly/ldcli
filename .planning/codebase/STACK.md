# Technology Stack

**Analysis Date:** 2026-05-11

## Languages

**Primary:**
- Go 1.23.0 - All backend CLI logic, dev server HTTP service, code generation pipeline
- TypeScript 5.2.2 - Dev server embedded React frontend (`internal/dev_server/ui/src/`)

**Secondary:**
- Go templates (`text/template`) - OpenAPI resource command generation (`cmd/resources/resource_cmds.tmpl`)

## Runtime

**Environment:**
- Go toolchain (CGO enabled; required for SQLite via `github.com/mattn/go-sqlite3`)
- Node.js LTS (CI uses `lts/*`; npm publish workflows use 20.x) - frontend build only

**Package Manager:**
- Go modules (`go.mod` / `go.sum`) — vendor directory not present by default; `make vendor` creates it
- npm (root `package.json` wraps binary distribution via `@go-task/go-npm`)
- npm (inner `internal/dev_server/ui/package.json`) — dev server frontend

**Lockfile:**
- `go.sum` — present
- `package-lock.json` — present at repo root and in `internal/dev_server/ui/`

## Frameworks

**Core CLI:**
- `github.com/spf13/cobra` v1.9.1 - Command/subcommand routing, help text, flag binding
- `github.com/spf13/viper` v1.21.0 - Config file + env var + flag precedence chain
- `github.com/spf13/pflag` v1.0.10 - POSIX-style flag parsing (used by Cobra)

**Dev Server HTTP:**
- `github.com/gorilla/mux` v1.8.1 - Router for dev server (`internal/dev_server/dev_server.go`)
- `github.com/gorilla/handlers` v1.5.2 - CORS, recovery, logging middleware
- `github.com/oapi-codegen/oapi-codegen/v2` v2.4.1 - Generates `internal/dev_server/api/server.gen.go` from `internal/dev_server/api/api.yaml`
- `github.com/oapi-codegen/runtime` v1.1.2 - Runtime support for generated server code

**TUI (Interactive Quickstart):**
- `github.com/charmbracelet/bubbletea` v1.3.6 - Elm-style TUI framework (`internal/quickstart/`, `cmd/quickstart.go`)
- `github.com/charmbracelet/bubbles` v0.21.0 - TUI components (list, help, key bindings)
- `github.com/charmbracelet/lipgloss` v1.1.1-0.20250404203927 - Terminal styling
- `github.com/charmbracelet/glamour` v0.10.0 - Markdown rendering in terminal (`cmd/resources/resources.go`, `cmd/resources/resource_cmds.go`)

**Frontend (dev server UI):**
- React 18.3.1 - UI framework (`internal/dev_server/ui/src/`)
- React Router 7.12.0 - SPA routing
- Vite 6.4.1 - Build tool; `vite-plugin-singlefile` bundles into one HTML file embedded in Go binary
- TypeScript 5.2.2

**OpenAPI / Code Generation:**
- `github.com/getkin/kin-openapi` v0.127.0 - Parses `ld-openapi.json` to drive template generation (`cmd/resources/resources.go`)
- `github.com/iancoleman/strcase` v0.3.0 - Case conversion during codegen
- Go `text/template` + `go/format` - Template execution and source formatting (`cmd/resources/gen_resources.go`)

**Testing:**
- `github.com/stretchr/testify` v1.11.1 - Assertions and test helpers
- `go.uber.org/mock` v0.5.2 - Mock generation via `mockgen`; mocks in `*/mocks/` packages
- Vitest v2.1.9 - Frontend unit test runner
- `@testing-library/react` v16.0.1 - React component testing

**Build/Dev:**
- GoReleaser v2 (`.goreleaser.yaml`) - Cross-compilation and release artifact building
- goreleaser-cross Docker image - Provides cross-compiler toolchains (musl, mingw, osxcross) for CGO cross-compilation
- `release-please` (googleapis/release-please-action v4.4.0) - Automated changelog and GitHub release management
- pre-commit (`.pre-commit-config.yaml`) - Git hook runner for `go fmt`, `go mod tidy`, frontend tests/build

## Key Dependencies

**Critical:**
- `github.com/launchdarkly/api-client-go/v14` v14.0.0 - Auto-generated Go client for the LaunchDarkly REST API; used by `internal/client/client.go` for all resource commands
- `github.com/launchdarkly/go-server-sdk/v7` v7.13.4 - Go server-side LD SDK; used by dev server to stream all flag state (`internal/dev_server/adapters/sdk.go`)
- `github.com/launchdarkly/go-sdk-common/v3` v3.4.0 - Shared LD SDK types (ldcontext, ldvalue)
- `github.com/launchdarkly/sdk-meta/api` v0.4.8 - SDK metadata (names, instructions) for quickstart (`internal/quickstart/`)
- `github.com/mattn/go-sqlite3` v1.14.28 - SQLite driver (CGO); required for dev server local storage (`internal/dev_server/db/`)

**Infrastructure:**
- `github.com/adrg/xdg` v0.5.3 - XDG Base Directory spec; resolves config and state paths (`internal/config/config.go`, `internal/dev_server/dev_server.go`)
- `github.com/mitchellh/go-homedir` v1.1.0 - Fallback home directory detection
- `github.com/google/uuid` v1.6.0 - UUID generation for analytics client ID
- `github.com/pkg/browser` v0.0.0-20240102092130 - Opens browser for login/signup flows
- `github.com/pkg/errors` v0.9.1 - Structured error wrapping
- `github.com/samber/lo` v1.51.0 - Generic functional helpers
- `golang.org/x/term` v0.33.0 - Terminal detection (e.g., TTY check for output formatting)
- `gopkg.in/yaml.v3` v3.0.1 - Config file serialization (`internal/config/config.go`)

**Frontend:**
- `@launchpad-ui/components` 0.4.4, `@launchpad-ui/core` 0.49.22, `@launchpad-ui/icons` 0.18.13, `@launchpad-ui/tokens` 0.11.3 - LaunchDarkly's internal design system
- `launchdarkly-js-client-sdk` 3.4.0 - Type definitions for flag values used in the dev server UI
- `fuzzysort` 3.0.2 - Fuzzy search for flag list
- `react-window` 1.8.10 - Virtualized list rendering for large flag lists
- `lodash` 4.17.23 - Utility functions

## Configuration

**CLI configuration file:**
- Path: `$XDG_CONFIG_HOME/ldcli/config.yml` (default: `~/.config/ldcli/config.yml`)
- Managed by: `internal/config/config.go`
- Fields: `access-token`, `analytics-opt-out`, `base-uri`, `dev-stream-uri`, `environment`, `flag`, `output`, `project`

**Precedence order:**
1. CLI flags
2. Environment variables prefixed `LD_` (via Viper)
3. Config file (`~/.config/ldcli/config.yml`)

**Default endpoints (defined in `cmd/cliflags/flags.go`):**
- `BaseURIDefault = "https://app.launchdarkly.com"`
- `DevStreamURIDefault = "https://stream.launchdarkly.com"`
- `PortDefault = "8765"` (dev server local port)

**Build:**
- `.goreleaser.yaml` — cross-compilation targets: linux (amd64, arm64, 386), darwin (amd64, arm64), windows (amd64, arm64, 386)
- `CGO_ENABLED=1` required in all builds (SQLite)
- `-X 'main.version={{.Version}}'` injects version at link time

## Platform Requirements

**Development:**
- Go 1.23+
- CGO-capable C compiler (gcc/clang) for SQLite
- Node.js LTS + npm (for dev server frontend work)
- `make install-hooks` to enable pre-commit checks

**Production:**
- Single static binary (Linux: statically linked via musl; macOS/Windows: dynamically linked but self-contained)
- No external runtime dependencies; SQLite and frontend assets are embedded
- Dev server SQLite databases stored in XDG state directory: `$XDG_STATE_HOME/ldcli/dev_server.db` and `$XDG_STATE_HOME/ldcli/dev_server_events.db`

---

*Stack analysis: 2026-05-11*
