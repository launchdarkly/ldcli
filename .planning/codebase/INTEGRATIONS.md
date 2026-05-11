# External Integrations

**Analysis Date:** 2026-05-11

## APIs & External Services

**LaunchDarkly REST API:**
- Used for all resource management commands (flags, environments, projects, members, access tokens, etc.)
- SDK/Client: `github.com/launchdarkly/api-client-go/v14` — instantiated in `internal/client/client.go`
- Auth: Bearer token from `access-token` config/flag (env var: `LD_ACCESS_TOKEN`)
- Base URL: configurable; default `https://app.launchdarkly.com` (`cmd/cliflags/flags.go`)
- API commands are auto-generated from `ld-openapi.json` into `cmd/resources/resource_cmds.go`
- Spec source: downloaded via `make openapi-spec-download` from `https://app.launchdarkly.com/api/v2/openapi.json`

**LaunchDarkly Streaming Service (dev server):**
- Used by the dev server to fetch authoritative flag state for a project/environment
- SDK/Client: `github.com/launchdarkly/go-server-sdk/v7` — instantiated per-request in `internal/dev_server/adapters/sdk.go`
- Auth: SDK key (passed at runtime when adding a project to the dev server)
- Stream URL: configurable; default `https://stream.launchdarkly.com` (`cmd/cliflags/flags.go`, flag `--dev-stream-uri`)
- Relay Proxy endpoint is also supported as an alternative to direct LD streaming

**LaunchDarkly Internal Tracking API (analytics):**
- Used for telemetry on CLI command usage; opt-out supported via `analytics-opt-out` config
- Client: custom HTTP client in `internal/analytics/client.go`
- Auth: user's access token sent as `Authorization` header
- Endpoint: `POST <baseURI>/internal/tracking`
- Events tracked: `CLI Command Run`, `CLI Command Completed`, `CLI Setup Step Started`, `CLI Setup SDK Selected`, `CLI Setup Flag Toggled`
- User-Agent: `launchdarkly-cli/<version>`
- Calls are async (goroutine); `Client.Wait()` flushes pending events

**LaunchDarkly Device Authorization API (OAuth login flow):**
- Implements device authorization grant for `ldcli login`
- Client: `internal/resources` unauthenticated HTTP client
- Endpoints (relative to `baseURI`):
  - `POST /internal/device-authorization` — creates device code + verification URI
  - `POST /internal/device-authorization/token` — polls for access token
- Client ID: hardcoded `e6506150369268abae3ed46152687201` (`internal/login/login.go`)
- Browser: opened via `github.com/pkg/browser` to show the verification URI

**LaunchDarkly Observability API (sourcemaps upload):**
- Used by `ldcli sourcemaps upload` for JavaScript sourcemap upload to LD error monitoring
- Client: custom HTTP client in `cmd/sourcemaps/upload.go`
- Base URL: `https://pri.observability.app.launchdarkly.com` (overridable via `--backend-url`)
- Uses GraphQL for `get_source_map_upload_urls_ld` query; then uploads files directly to presigned URLs
- Auth: LD access token used to GET project credential from `<baseURI>/api/v2/projects/<key>`

**LaunchDarkly Signup:**
- `ldcli signup` opens browser to `<baseURI>/signup` via `github.com/pkg/browser`
- No API calls; browser-only flow (`cmd/signup/signup.go`)

## Data Storage

**Databases:**
- SQLite (dev server flag state store)
  - Location: `$XDG_STATE_HOME/ldcli/dev_server.db`
  - Client: `github.com/mattn/go-sqlite3` (CGO), standard `database/sql` interface
  - Tables: `projects`, `overrides`, `available_variations`
  - Implementation: `internal/dev_server/db/sqlite.go`
  - Schema migrations run inline on startup in `runMigrations()`
  - Backup/restore: `internal/dev_server/db/backup/` package; backup written to temp file, streamed via HTTP

- SQLite (dev server events store)
  - Location: `$XDG_STATE_HOME/ldcli/dev_server_events.db`
  - Implementation: `internal/dev_server/events_db/sqlite.go`
  - Stores debug session events for the UI debug view

**File Storage:**
- Config file: `$XDG_CONFIG_HOME/ldcli/config.yml` (read/written by `internal/config/config.go`)
- No object storage or remote file storage

**Caching:**
- None (each dev server SDK sync creates a fresh client; no in-process cache for API responses)

## Authentication & Identity

**Auth Provider:**
- Custom device authorization flow against the LaunchDarkly API (`internal/login/login.go`)
- No third-party OAuth provider (Auth0, Okta, etc.) involved client-side; the LD platform handles identity internally
- Access token stored in config file after login: `$XDG_CONFIG_HOME/ldcli/config.yml` field `access-token`

## Monitoring & Observability

**Error Tracking:**
- None (no Sentry, Datadog, etc.)

**Logs:**
- Dev server: Go standard `log` package to stdout; combined access log via `gorilla/handlers.CombinedLoggingHandler`
- CLI commands: stderr for errors, stdout for output; no structured logging library

## CI/CD & Deployment

**Hosting:**
- Docker Hub: `launchdarkly/ldcli` (amd64 and arm64v8 multi-arch manifest)
  - Built via `.goreleaser.yaml` using `Dockerfile.goreleaser` (Alpine 3.19.1 base)
  - Tags: `latest`, `v{Major}`, `{Version}`, per-arch suffixed variants

**Distribution channels:**
1. **GitHub Releases** — tarball artifacts built by GoReleaser; release lifecycle managed by `release-please` (`.github/workflows/release-please.yml`)
2. **Homebrew** — formula pushed to `launchdarkly/homebrew-tap` repo, `Formula/` directory, via GoReleaser (`brews` section in `.goreleaser.yaml`)
3. **Docker Hub** — `launchdarkly/ldcli` image pushed via GoReleaser
4. **npm** — `@launchdarkly/ldcli` package; wraps binary download via `@go-task/go-npm`; published via OIDC trusted publisher (no static npm token) in `release-please.yml`

**CI Pipeline:**
- GitHub Actions
  - `go.yml` — build + test on push/PR to main
  - `dev-server-ui.yml` — frontend lint, prettier, test, build
  - `release-please.yml` — automated release creation, GoReleaser build, Docker push, npm publish
  - `manual-publish.yml` — manually triggered release/publish workflow
  - `check-openapi-updates.yml` — periodic check for LD OpenAPI spec updates
  - `dependency-scan.yml` — dependency vulnerability scanning

**Secrets (GitHub Actions):**
- `HOMEBREW_DEPLOY_KEY` — SSH deploy key for pushing to `homebrew-tap` repo
- `DOCKER_HUB_USERNAME` / `DOCKER_HUB_TOKEN` — fetched from AWS SSM Parameter Store via `launchdarkly/gh-actions/actions/release-secrets`
- `GITHUB_TOKEN` — standard token for GitHub Releases and npm OIDC

## Webhooks & Callbacks

**Incoming:**
- None (the CLI is not a server in production use)

**Outgoing:**
- `POST <baseURI>/internal/tracking` — analytics events (fire-and-forget, async)
- `POST <baseURI>/internal/device-authorization` — login device auth initiation
- `POST <baseURI>/internal/device-authorization/token` — login token polling

## OpenAPI Code Generation Pipeline

The CLI resource commands are generated from the LaunchDarkly public OpenAPI spec:

1. **Spec source:** `ld-openapi.json` (downloaded from `https://app.launchdarkly.com/api/v2/openapi.json`)
2. **Parser:** `github.com/getkin/kin-openapi/openapi3` in `cmd/resources/resources.go`; `GetTemplateData()` extracts resource metadata from spec tags and operations
3. **Template:** `cmd/resources/resource_cmds.tmpl` — Go source template
4. **Generator:** `cmd/resources/gen_resources.go` (build tag: `gen_resources`) — reads spec, executes template, formats with `go/format`, writes `cmd/resources/resource_cmds.go`
5. **Trigger:** `//go:generate go run resources/gen_resources.go` in `cmd/root.go`; run via `make generate`
6. **Output:** `cmd/resources/resource_cmds.go` (~613KB generated file; do not edit manually)

Dev server API is separately generated:
- Spec: `internal/dev_server/api/api.yaml`
- Config: `internal/dev_server/api/oapi-codegen-cfg.yaml` (gorilla-server mode)
- Output: `internal/dev_server/api/server.gen.go`
- Trigger: `//go:generate` in `internal/dev_server/api/server.go`

## Embedded Frontend Assets

The dev server React UI is compiled to a single HTML file and embedded into the Go binary:

- Build: `cd internal/dev_server/ui && npm run build` produces `internal/dev_server/ui/dist/`
- `vite-plugin-singlefile` inlines all JS/CSS into `dist/index.html`
- Embedded via `//go:embed all:dist` in `internal/dev_server/ui/asset_handler.go`
- Served as a virtual filesystem by `ui.AssetHandler` at `/ui/` path in the dev server

---

*Integration audit: 2026-05-11*
