# ldcli surfaces for Dependabot verification

This is a lookup table for [`dependabot-upgrade-verification.md`](dependabot-upgrade-verification.md). The procedures and rules live there. Paths and packages here were accurate when written. Confirm them with a search before relying on them.

## What CI runs

Read `.github/workflows/` for the current list. The two that matter for dependency bumps:

| Workflow | What it proves | What it does not |
| --- | --- | --- |
| `go.yml` | `go build .`, pre-commit hooks, `go test ./...` | Never runs the built binary or the dev-server over HTTP |
| `dev-server-ui.yml` | `npm ci`, lint, Prettier, `npm test`, `npm run build`, and that the build leaves no diff in the checked-in `dist/` | Never loads the UI in a browser |

Neither workflow talks to LaunchDarkly.

## Dev-server facts

- `dev-server start` requires `--access-token`. `cmd/root.go` exempts only a short list of commands from that requirement, and `dev-server` is not on it.
- Default port `8765` (`cmd/cliflags`). Verification runs use a different port.
- Databases: `$XDG_STATE_HOME/ldcli/dev_server.db` and `dev_server_events.db` (`internal/dev_server/dev_server.go`).
- Config: `$XDG_CONFIG_HOME/ldcli/config.yml` (`internal/config`). It is created if missing.
- Analytics: sent from `internal/analytics` unless `--analytics-opt-out` or `LD_ANALYTICS_OPT_OUT=true` is set.
- The UI is served at `/ui` from the checked-in `internal/dev_server/ui/dist/` through `go:embed` (`internal/dev_server/ui/asset_handler.go`). Routes are defined in `internal/dev_server/ui/src/App.tsx`.
- UI runtime dependency bumps historically needed a rebuilt `dist/index.html` committed alongside the lockfile change.
- Check how much the Vitest suite in `internal/dev_server/ui/src/__tests__/` covers before treating `npm test` as UI coverage.

## Package → mode

### Go modules (repo root)

| Package | First-party surface | Mode |
| --- | --- | --- |
| `github.com/spf13/cobra` | Every command in `cmd/` | `CLI_SMOKE` |
| `github.com/spf13/pflag` | Flag sets; wrapped usage in `cmd/templates.go` | `CLI_SMOKE` |
| `github.com/spf13/viper` | Flag, env (`LD_` prefix), and config binding | `CLI_SMOKE`, including one env var and one config value |
| `golang.org/x/term` | `GetSize` for help wrapping; `IsTerminal` for output defaults and prompts | `CLI_SMOKE`, piped and real TTY |
| `github.com/charmbracelet/bubbletea`, `bubbles`, `lipgloss` | Interactive TUI flows (`cmd/setup`, `internal/quickstart`) | `CLI_SMOKE` in a real TTY; escalate if you cannot get one |
| `github.com/charmbracelet/glamour` | Markdown rendering of resource command help (`cmd/resources`) | `CLI_SMOKE`: `--help` for a few resource commands |
| `github.com/mattn/go-sqlite3` | `internal/dev_server/db`, `events_db`, `db/backup` | `STORE_SMOKE` (CGO) |
| `github.com/gorilla/mux`, `gorilla/handlers` | Dev-server routing, CORS, logging | `STORE_SMOKE`, plus one `/dev` API request |
| `github.com/launchdarkly/go-server-sdk/*`, `go-sdk-common` | Dev-server SDK adapters and model (`internal/dev_server`), `internal/setup`, `sdk_active` | `STORE_SMOKE` plus `CLI_SMOKE`; project sync needs a real token, so record that as residual risk |
| `go.uber.org/mock` | `tools.go` and generated mocks | `TEST_ONLY` |
| `github.com/oapi-codegen/*`, `github.com/getkin/kin-openapi` | Code generation for the dev-server API and resource commands | `BUILD_ONLY`; escalate if regenerated output would change |
| `golang.org/x/net`, `x/oauth2`, `x/sys`, other `x/*` | Usually transitive | Search first; `NO_EXTRA` if nothing in first-party code imports it |

### npm: `internal/dev_server/ui`

| Package | Mode |
| --- | --- |
| `react`, `react-dom`, `react-router` | `UI_COMPUTER_USE` |
| `@launchpad-ui/*` | `UI_COMPUTER_USE`; look for unstyled or missing components |
| `launchdarkly-js-client-sdk` | `UI_COMPUTER_USE` |
| `lodash`, `fuzzysort`, `react-window` | `UI_COMPUTER_USE`: flags list, search, long lists |
| `vite`, `vite-plugin-*`, `rollup`, `typescript` | `BUILD_ONLY`, plus a `dist/` diff check |
| `vitest`, `@testing-library/*` | `TEST_ONLY` |
| `prettier`, `eslint`, `eslint-plugin-*`, `typescript-eslint` | `BUILD_ONLY` |
| Lockfile-only transitive packages | `NO_EXTRA`, unless a search finds a first-party import |

### npm: repo root

| Package | Mode |
| --- | --- |
| `@go-task/go-npm` | `INSTALL_SMOKE`. The root package only wraps the release binary for `npm install -g @launchdarkly/ldcli`. |

### GitHub Actions and Docker

| Package | Mode |
| --- | --- |
| `actions/*` | `CI_ONLY`; majors are an escalation trigger |
| `googleapis/release-please-action` | `CI_ONLY`. Never trigger a release. |
| `launchdarkly/gh-actions/*` | `CI_ONLY` |
| Base image in `Dockerfile.goreleaser` | `CI_ONLY`; run `docker build` if Docker is available |
