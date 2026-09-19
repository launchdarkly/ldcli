# ldcli surfaces for Dependabot verification

Read this after classifying the PR. It is a lookup table, not a second policy. The policy lives in `dependabot-upgrade-verification.md`.

## What CI already runs

| Workflow | Trigger | What it proves |
| --- | --- | --- |
| `.github/workflows/go.yml` | every PR | `go build .`, pre-commit, `go test ./...` |
| `.github/workflows/dev-server-ui.yml` | every PR | `npm ci`, lint, prettier, `npm test`, `npm run build`, no leftover UI diff |
| `.github/workflows/dependency-scan.yml` | scheduled / selected | security scan, not product behavior |

CI does **not** start `ldcli`, does **not** open the embedded UI, and does **not** talk to LaunchDarkly.

## How to boot the product locally

```bash
make build
./ldcli dev-server start --port 8765 --access-token dummy-for-local-smoke
```

- `--access-token` is required on `dev-server start` (not in `authExemptCommands` in `cmd/root.go`). A dummy value is fine if you omit `--project` and `--source`.
- Default port: `8765` (`cmd/cliflags.PortDefault`).
- SQLite paths: XDG state `ldcli/dev_server.db` and `ldcli/dev_server_events.db` (`internal/dev_server/dev_server.go`). On Linux that is typically `~/.local/state/ldcli/`.
- UI: `http://127.0.0.1:8765/ui` (redirects to `/ui/flags`). A successful empty boot returns HTTP 200 and a large single-file HTML bundle.
- The binary serves `internal/dev_server/ui/dist` via `//go:embed` (`internal/dev_server/ui/asset_handler.go`). An npm bump is not in the shipped UI until you `npm run build` **and** `make build`.
- Project sync only happens if both `--project` and the source-environment flag are set. Without a real token, start with no project flags and exercise the empty UI / local store.
- Stale Dependabot branches are common (rebases get disabled after 30 days). Count commits behind `main` before treating a smoke as evidence about current `cmd/`.

UI routes (`internal/dev_server/ui/src/App.tsx`):

| Route | Page |
| --- | --- |
| `/ui/flags` | Flags + project/environment selectors |
| `/ui/events` | Events table |
| `/ui/debug-sessions` | Debug sessions |
| `/ui/debug-sessions/:key/events` | Session events |

Vitest coverage today is thin (`SubmitButton` only). A passing `npm test` is not a UI smoke test.

## Ecosystem → mode

ldcli Dependabot covers `gomod` (repo root), `npm` (`/` and `/internal/dev_server/ui`), `github-actions`, and `docker`.

### Go modules

| Package | First-party surface | Mode | Extra check |
| --- | --- | --- | --- |
| `github.com/spf13/cobra` | Every command under `cmd/` | `CLI_SMOKE` | Built binary help tree + `go test ./cmd/...` |
| `github.com/spf13/pflag` | Flag sets, usage wrapping in `cmd/templates.go` | `CLI_SMOKE` | Same as cobra; watch `ParseErrorsWhitelist` / `ParseErrorsAllowlist` breaks |
| `github.com/spf13/viper` | Flag/env/config binding | `CLI_SMOKE` | `ldcli config` + a command that reads a bound flag |
| `golang.org/x/term` | `cmd/templates.go` `GetSize`; `cmd/root.go` / `cmd/setup` / analytics `IsTerminal` | `CLI_SMOKE` | Piped help (fallback 80) + TTY help if computer use can open a terminal |
| `github.com/mattn/go-sqlite3` | `internal/dev_server/db/sqlite.go`, `events_db/sqlite.go`, `db/backup` | `STORE_SMOKE` | Store tests + `dev-server start` + UI load + db file created. CGO required |
| `go.uber.org/mock` | `tools.go` + generated mocks under `internal/dev_server/**/mocks` | `TEST_ONLY` | `go test ./...`; computer use adds nothing |
| `github.com/oapi-codegen/oapi-codegen` | generated API server | `ESCALATE` if the bump wants regenerate; else `BUILD_ONLY` | Do not silently regenerate `resource_cmds.go` / `server.gen.go` |
| `golang.org/x/net` | transitive + any direct HTTP | `CLI_SMOKE` if imported by first-party net code; else `NO_EXTRA` | Changelog for HTTP/2 / proxy CVEs; no UI |

### npm (`internal/dev_server/ui`)

| Package | Mode | Extra check |
| --- | --- | --- |
| `react`, `react-dom`, `react-router` | `UI_COMPUTER_USE` | Rebuild embed, boot server, click all three nav routes. A router major is `ESCALATE` until the app still renders |
| `@launchpad-ui/core`, `components`, `icons`, `tokens` | `UI_COMPUTER_USE` | Same; look for unstyled / missing primitives |
| `launchdarkly-js-client-sdk` | `UI_COMPUTER_USE` | UI must still boot; client-side evaluate may be empty without a client-side ID |
| `lodash`, `fuzzysort`, `react-window` | `UI_COMPUTER_USE` | Flags list / search / virtualized rows |
| `vite`, `vite-plugin-*`, `rollup`, `typescript` | `BUILD_ONLY` | `npm run build` |
| `vitest`, `@testing-library/react` | `TEST_ONLY` | `npm test` |
| `prettier`, `eslint`, `typescript-eslint` | `BUILD_ONLY` | lint/format scripts already in UI CI — extra check is only if you suspect the hook itself broke |
| lockfile-only transitive (`ws`, `picomatch`, `dompurify` if not imported) | `NO_EXTRA` unless first-party code imports it | Confirm with grep before skipping |

### GitHub Actions / Docker

| Package | Mode | Extra check |
| --- | --- | --- |
| `actions/checkout`, `actions/setup-go`, `actions/setup-node`, `actions/setup-python` | `CI_ONLY` | Read the workflow. Majors that change default Node/Go setup are `ESCALATE` |
| `googleapis/release-please-action` | `CI_ONLY` | Do not run a release |
| `alpine` in `Dockerfile.goreleaser` | `CI_ONLY` | Optional: `docker build` if Docker is available; otherwise changelog + escalate native deps |
