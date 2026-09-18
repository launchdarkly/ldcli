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
./ldcli dev-server start --port 8765
```

- Default port: `8765` (`cmd/cliflags.PortDefault`).
- SQLite paths: XDG state `ldcli/dev_server.db` and `ldcli/dev_server_events.db` (`internal/dev_server/dev_server.go`).
- UI: `http://127.0.0.1:8765/ui` (redirects to `/ui/flags`).
- The binary serves `internal/dev_server/ui/dist` via `//go:embed` (`internal/dev_server/ui/asset_handler.go`). An npm bump is not in the shipped UI until you `npm run build` **and** `make build`.
- Project sync only happens if both `--project` and the source-environment flag are set. Without credentials, start with no project flags and exercise the empty UI / local store.

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
| `react`, `react-dom`, `react-router` | `UI_COMPUTER_USE` | Rebuild embed, boot server, click all three nav routes. Router majors (7 → 8) are `ESCALATE` until the app still renders |
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

## Worked examples (ldcli Dependabot PRs)

These are the classification answers a verification agent should reach. They are not merge approvals.

### [#726](https://github.com/launchdarkly/ldcli/pull/726) — `go-sqlite3` 1.14.28 → 1.14.47 (patch)

- **Mode:** `STORE_SMOKE`
- **Surface:** CGO SQLite driver for local dev-server + events DB + backup/restore.
- **CI already:** `go test ./...` includes `internal/dev_server/db/backup` and SDK tests that open a real sqlite store.
- **Gap:** CI never starts the HTTP server, never opens a second connection after process restart, never hits `/ui`.
- **Extra check:** targeted store tests, `make build`, `ldcli dev-server start`, curl `/ui/`, confirm `dev_server.db` exists, optional computer-use load of the empty UI, restart once.
- **Video:** yes if the UI process is up — prove `/ui/flags` renders after the driver bump. Skip if CGO cannot build.
- **Watch:** CString leak / callback ordering fixes are driver-internal; a boot + read/write is the available extra signal, not a proof of those C bugs.

### [#725](https://github.com/launchdarkly/ldcli/pull/725) — `cobra` 1.9.1 → 1.10.2 (minor)

- **Mode:** `CLI_SMOKE`
- **Surface:** command tree, help, completion, usage templates.
- **CI already:** command-construction unit tests.
- **Gap:** CI does not execute the shipped binary's help/completion entrypoints. Cobra 1.10.0 pulled a pflag rename (`ParseErrorsWhitelist` → `ParseErrorsAllowlist`, restored as deprecated in pflag 1.0.9 / cobra 1.10.1).
- **Extra check:** `make build` + help for root, `completion`, `dev-server`, `flags`, `setup`; `go test ./cmd/...`. Grep for `ParseErrorsWhitelist` / `ParseErrorsAllowlist`.
- **Video:** optional. A 20-second TTY help walk is enough; a browser is not.

### [#626](https://github.com/launchdarkly/ldcli/pull/626) — `golang.org/x/term` 0.33.0 → 0.36.0 (minor)

- **Mode:** `CLI_SMOKE`
- **Surface:** `term.GetSize` for wrapped flag help; `term.IsTerminal` for output format and setup prompts.
- **CI already:** `TestNewRootCommandNilIsTerminalRejects`, non-TTY output tests. Those inject a fake `IsTerminal`.
- **Gap:** CI is not a real TTY, so `GetSize` always takes the width-80 fallback.
- **Extra check:** piped `./ldcli --help` (fallback path) plus, if computer use can open a terminal, run help in that TTY. Do not claim you tested wrapping unless you saw a TTY width.
- **Video:** only for the TTY case. A piped command in the agent log is not a video.

### [#621](https://github.com/launchdarkly/ldcli/pull/621) — `go.uber.org/mock` 0.5.2 → 0.6.0 (minor)

- **Mode:** `TEST_ONLY`
- **Surface:** `mockgen` in `tools.go` and generated mocks. No production import.
- **CI already:** `go test ./...` is the entire product impact.
- **Gap:** none that a GUI can close. v0.6.0 adds archive-mode mockgen and a go1.25 tools bump.
- **Extra check:** `go test ./...`. Optional `go generate` on one mock directive; expect an empty diff.
- **Video:** none — computer use would not add signal.

## Nearby PRs that change the mode

Use these when the automation is pointed at the current Dependabot backlog, not only the four above.

| PR | Package | Mode |
| --- | --- | --- |
| #729 | `react-router` 7.12.0 → 8.0.1 | `ESCALATE` + `UI_COMPUTER_USE` (major, nav will break if incompatible) |
| #723 | `@launchpad-ui/core` 0.49.22 → 0.59.17 | `UI_COMPUTER_USE` |
| #724 | `prettier` 3.3.2 → 3.8.4 | `BUILD_ONLY` |
| #728 | `rollup` lockfile | `BUILD_ONLY` |
| #721 / #717 / #719 | GitHub Actions majors | `CI_ONLY` or `ESCALATE` |
| #716 | `alpine` 3.19 → 3.24 | `CI_ONLY` |
