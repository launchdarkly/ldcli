# Codebase Concerns

**Analysis Date:** 2026-05-11

---

## Tech Debt

**Analytics errors are silently swallowed:**
- Issue: All four error paths in the analytics HTTP goroutine have `// TODO: log error` comments and do nothing on failure. JSON marshal errors, `http.NewRequest` errors, HTTP send errors, and response body read errors are all discarded with a `nolint:staticcheck` suppression.
- Files: `internal/analytics/client.go:63–96`
- Impact: Analytics failures are invisible; impossible to debug why tracking stops working in production
- Fix approach: Add structured logging calls (`log.Printf`) at each error site; remove `nolint:staticcheck` suppressions

**OpenAPI-generated code never edited but may lag spec:**
- Issue: `cmd/resources/resource_cmds.go` (11,111 lines) is committed and must be regenerated via `make openapi-spec-update` whenever `ld-openapi.json` changes. There is a CI check (`check-openapi-updates.yml`) but it only runs on schedule — it does not block PRs that modify related Go code without regenerating.
- Files: `cmd/resources/resource_cmds.go`, `ld-openapi.json`, `.github/workflows/check-openapi-updates.yml`
- Impact: Generated code can drift from the live LD API without a compile-time signal
- Fix approach: Add the OpenAPI drift check as a required status check on PRs, or switch to always-regenerate-on-CI strategy

**Hard-coded OAuth client ID in source:**
- Issue: The device authorization OAuth client ID is a hard-coded string constant (`ClientID = "e6506150369268abae3ed46152687201"`) embedded in the source. This is not rotatable without a code change and release.
- Files: `internal/login/login.go:14`
- Impact: Cannot rotate the client ID without cutting a release; if it needs to be per-environment (staging vs prod), this approach breaks
- Fix approach: Move to a build-time injected constant (ldflags), or load from a configuration file

**Config file has a dead `Filename` constant:**
- Issue: `internal/config/config.go` declares `const Filename = ".ldcli-config.yml"` but the actual config file path is computed by `GetConfigFile()` and resolves to `~/.config/ldcli/config.yml` (XDG). The `Filename` constant is never used in production code paths.
- Files: `internal/config/config.go:17`
- Impact: Confusion about which path is authoritative; new contributors may use `Filename` incorrectly
- Fix approach: Delete `Filename` constant or replace with `ConfigFilename = "config.yml"` matching actual behavior

**Quickstart SDK code examples are hard-coded:**
- Issue: `sdkExamples` map in `internal/quickstart/choose_sdk.go:74` hard-codes GitHub example URLs for a subset of SDKs. The comment acknowledges these should come from `sdkmeta` but the integration is deferred.
- Files: `internal/quickstart/choose_sdk.go:72–81`
- Impact: Adding a new SDK or changing a repository URL requires a code change; other SDKs silently have no example link
- Fix approach: Track this in sdkmeta project and consume it here once available; in the interim, document the manual update step

**Viper global state shared across the process:**
- Issue: The entire CLI uses a single global `viper.GetString()` / `viper.SetConfigFile()` instance rather than scoped Viper instances. This makes parallel test execution unsafe (env vars set in one test bleed into another) and prevents running multiple root commands in the same process.
- Files: `cmd/root.go:183–365`, `cmd/cmdtest.go:63–68` (uses `os.Setenv` for test setup which affects global Viper)
- Impact: Flaky tests if parallelism is added; `t.Parallel()` in command-layer tests is blocked
- Fix approach: Create a scoped `viper.New()` instance in `NewRootCommand` and pass it down; see `cmd/root.go:176` for where reading starts

**`internal/config` imports `cmd/cliflags` (layer violation):**
- Issue: `internal/config/config.go` imports `github.com/launchdarkly/ldcli/cmd/cliflags` to validate allowed config keys. This means an `internal/` package depends on a `cmd/` package, which inverts the intended dependency direction.
- Files: `internal/config/config.go:13`, `internal/config/config.go:59–60`
- Impact: Creates a circular potential; `cmd/` packages that import `internal/config/` now transitively depend on `cmd/cliflags/` through `internal/`
- Fix approach: Move the valid-keys list into `internal/config/` (as noted by the `// TODO: move this list to this package?` comment at line 59), removing the import of `cmd/cliflags`

---

## Known Bugs / Fragile Logic

**`rows.Close()` missing in two SQLite query loops:**
- Symptoms: `*sql.Rows` from `database.Query` in `GetDevProjectKeys` (line 29) and `GetAvailableVariationsForProject` (line 228) are iterated without `defer rows.Close()`. On early-return error paths, the SQLite connection is not released.
- Files: `internal/dev_server/db/sqlite.go:28–43`, `internal/dev_server/db/sqlite.go:228–272`
- Trigger: Any error during row scan — the connection stays open until GC, which can exhaust the SQLite connection pool under load
- Workaround: None; `GetOverridesForProject` at line 285 shows the correct pattern with `defer rows.Close()`

**Ad-hoc SQLite migration via `ALTER TABLE` string matching:**
- Symptoms: The `runMigrations` function checks for migration success by testing if the error message contains `"duplicate column name"` — this is a string match on a SQLite error message, not a schema version table.
- Files: `internal/dev_server/db/sqlite.go:475–479`
- Trigger: SQLite error message format change, or a new migration needed for the same column
- Workaround: None; the migration runs on every startup; adding new columns requires adding new string-match exception handling

**Panic in streaming observer goroutines kills the dev server:**
- Symptoms: In `stream_server_fdv2.go` and `stream_client_flags.go`, errors during SSE event marshaling cause `panic(errors.Wrap(err, ...))`. While a `handlers.RecoveryHandler` is registered at the HTTP middleware level, the panics occur inside goroutines launched by observer callbacks — these goroutines are not covered by the middleware recovery.
- Files: `internal/dev_server/sdk/stream_server_fdv2.go:76,85`, `internal/dev_server/sdk/stream_client_flags.go:62,75`, `internal/dev_server/sdk/stream_server_flags.go:66,75`
- Trigger: `json.Marshal` failure on flag state (e.g., a flag with a non-serializable value type)
- Workaround: The `handlers.RecoveryHandler` at `dev_server.go:70` will not catch panics in child goroutines

**`store_facade.go` panics on unknown errors, bypassing HTTP error handling:**
- Symptoms: `WriteError` in `sdk/store_facade.go` panics for any error that is not `model.ErrNotFound`. The recovery middleware is in-scope here (this is an HTTP handler context), but panics log a stack trace rather than returning a structured HTTP 500.
- Files: `internal/dev_server/sdk/store_facade.go:24–27`
- Trigger: Any unexpected store error (DB timeout, corrupted data) during a flag delivery request
- Workaround: The `handlers.RecoveryHandler` catches it; response will be a 500 but without the structured JSON the client expects

**`model/flags_state.go` panics on missing key during iteration:**
- Symptoms: The flag state iteration logic in `model/flags_state.go:22–23` has an explicit `panic("flag '" + key + "' not found")` for a condition it considers "should be impossible." If flag state is deserialized with a missing key, the dev server panics.
- Files: `internal/dev_server/model/flags_state.go:22–23`
- Trigger: Deserialized flag state where the key set and value map are out of sync (e.g., partial DB write)

**`streamingSdk` creates a new SDK client per-request:**
- Symptoms: `adapters/sdk.go:GetAllFlagsState` creates a new `ldsdk.MakeCustomClient` on every call and closes it when done. This means a fresh SDK connection to the LaunchDarkly streaming API is opened and closed for every flag sync operation — incurring a 5-second wait per request.
- Files: `internal/dev_server/adapters/sdk.go:41–62`
- Impact: Slow dev server startup and slow project sync; not a connection pool

---

## Security Considerations

**Access token stored in plaintext YAML config:**
- Risk: The LD access token is persisted to `~/.config/ldcli/config.yml` in plaintext YAML. If a user's home directory is world-readable or backed up to cloud storage, the token is exposed.
- Files: `internal/config/config.go:23`, `cmd/config/config.go` (set/write path)
- Current mitigation: None — no keychain integration, no file permission enforcement on write
- Recommendations: On write, `chmod 0600` the config file; document that users should treat the file as a secret

**OAuth client ID hard-coded in binary:**
- Risk: The device authorization client ID (`e6506150369268abae3ed46152687201`) is a public constant embedded in every release binary. If the LD platform does not enforce PKCE or binding, an attacker can impersonate the CLI.
- Files: `internal/login/login.go:14`
- Current mitigation: The device authorization flow itself does not send a client secret; this matches the public-client OAuth 2.0 device flow spec. Risk is limited but the ID cannot be rotated without a release.
- Recommendations: Document rotation procedure; consider build-time injection via ldflags

**Dev server binds to 0.0.0.0 (all interfaces):**
- Risk: `dev_server.go:112` binds the HTTP server to `0.0.0.0:{port}` rather than `127.0.0.1:{port}`. On a developer machine on a shared network, the dev server (which holds the LD access token in memory and proxies flag data) is reachable from other hosts.
- Files: `internal/dev_server/dev_server.go:112`
- Current mitigation: No authentication on the `/dev/*` management API or `/ui/` routes
- Recommendations: Default to `127.0.0.1`; make the bind address a configurable flag

**Analytics sends access token in Authorization header to LD tracking endpoint:**
- Risk: The analytics client sends the user's LD access token in every tracking request (`internal/analytics/client.go:80`). This is correct for the LD API but means all analytics errors (including network failures to third-party interceptors) can expose the token in logs if HTTP debugging is enabled.
- Files: `internal/analytics/client.go:80–82`
- Current mitigation: Analytics is fire-and-forget; errors are not logged

---

## Performance Bottlenecks

**New SDK client created per project sync:**
- Problem: Every call to `adapters.Sdk.GetAllFlagsState` opens a new LaunchDarkly streaming SDK connection, waits up to 5 seconds for initialization, fetches all flags, then closes the connection.
- Files: `internal/dev_server/adapters/sdk.go:41–62`
- Cause: `streamingSdk` is stateless by design but the Go SDK is not designed for per-request lifecycle
- Improvement path: Cache a long-lived SDK client per `(sdkKey, streamingUrl)` pair; close on dev server shutdown

**SQLite used without WAL mode or connection pool limits:**
- Problem: `NewSqlite` opens the database with `sql.Open("sqlite3", dbPath)` with no PRAGMA settings. SQLite defaults to DELETE journal mode and a single writer, so concurrent reads and writes during active flag streaming will serialize or produce `SQLITE_BUSY` errors.
- Files: `internal/dev_server/db/sqlite.go:433`, `internal/dev_server/events_db/sqlite.go` (same pattern)
- Cause: No `PRAGMA journal_mode=WAL` or `SetMaxOpenConns(1)` to serialize writers
- Improvement path: Set `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000` after opening; call `db.SetMaxOpenConns(1)` for the writer connection

**React SPA embedded in Go binary bloats binary size:**
- Problem: The production SPA bundle (`internal/dev_server/ui/dist/index.html`) is a single-file bundle via `vite-plugin-singlefile`, embedded via `//go:embed all:dist` in every `ldcli` binary release even for users who never use the dev server.
- Files: `internal/dev_server/ui/asset_handler.go:10`, `internal/dev_server/ui/dist/`
- Cause: Architectural decision to bundle UI with CLI for zero-install experience
- Improvement path: Conditional build tags to exclude UI in lightweight builds; or lazy-load assets from a CDN at runtime

---

## Fragile Areas

**Generated `resource_cmds.go` (11,111 lines) is a manual regeneration step:**
- Files: `cmd/resources/resource_cmds.go`
- Why fragile: Must be regenerated via `make generate` or `make openapi-spec-update` whenever the API surface changes. The file is committed, so merge conflicts on this file are extremely painful. There is no automated regeneration in the standard PR workflow.
- Safe modification: Never edit `cmd/resources/resource_cmds.go` directly. Always go through `make openapi-spec-update`. Resolve conflicts by regenerating from the merged `ld-openapi.json`.
- Test coverage: Only two narrow tests exist for generated command behavior (`cmd/resources/resource_cmds_test.go`); one test is skipped (`t.Skip("TODO: add back when mock client is added")`)

**`cmd/login/` has no command-layer tests:**
- Files: `cmd/login/login.go`
- Why fragile: The login flow (device authorization, token polling with 120-attempt loop, browser open, config file write) has no `cmd/` layer test. `internal/login/login.go` has a test but it only tests the internal polling logic.
- Safe modification: Carefully trace through `cmd/login/login.go` before changing; no automated regression safety net at the command level
- Test coverage: `internal/login/login_test.go` covers only the internal polling loop; the command wrapper is untested

**`cmd/quickstart.go` and `internal/quickstart/` have no tests:**
- Files: `cmd/quickstart.go`, `internal/quickstart/`
- Why fragile: The interactive Bubbletea TUI flow (multi-step wizard with model updates) has no unit tests. The `cmd/quickstart.go` entry point uses `log.Fatal` directly, bypassing the standard error handling path.
- Safe modification: Test each Bubbletea model's `Update` function independently before modifying state transitions
- Test coverage: None detected

**`cmd/dev_server/` has minimal command-layer tests:**
- Files: `cmd/dev_server/dev_server.go`, `cmd/dev_server/start_server.go`, `cmd/dev_server/import_project.go`
- Why fragile: The `start_server` command hard-codes a `localhost` URL in `cmd/dev_server/dev_server.go:92` for the UI redirect; the `import_project` command also directly calls `xdg.StateFile` rather than accepting the DB path as a parameter, making it untestable without filesystem side effects
- Test coverage: `cmd/dev_server/start_server_test.go` and `import_project_test.go` exist but coverage is narrow

**Pre-commit hook requires `npm` for every commit:**
- Files: `AGENTS.md` (documents this), `.pre-commit-config.yaml`
- Why fragile: The `git/hooks/pre-commit` script (installed via `make install-hooks`) runs `npm test` and `npm run build` in `internal/dev_server/ui/` before every commit. Developers without Node.js installed or with a broken npm cache will have all commits blocked.
- Safe modification: Always run `npm ci` in `internal/dev_server/ui/` before committing; if only modifying Go code, pre-commit still runs the full frontend build
- Test coverage: This is enforced but creates onboarding friction

**Frontend UI has near-zero test coverage:**
- Files: `internal/dev_server/ui/src/__tests__/SubmitButton.test.tsx` (sole test file)
- Why fragile: Only `SubmitButton` has tests. `Flags.tsx` (360 lines), `FlagsPage.tsx` (216 lines), `Flag.tsx` (212 lines), `ProjectEditor.tsx` (156 lines), and all API integration code in `api.ts` are untested.
- Safe modification: Treat all UI components as fragile; manual testing required for any change
- Test coverage: 1 component tested out of ~15 source files

---

## Scaling Limits

**SQLite is a single-file DB with no migration framework:**
- Current capacity: Adequate for local single-developer use; designed for one concurrent writer
- Limit: Concurrent SDKs streaming from the dev server while the UI writes overrides will produce lock contention; no migration versioning means adding columns requires the `ALTER TABLE ... duplicate column name` workaround
- Scaling path: Add WAL mode; consider a proper migration framework (e.g., `golang-migrate`) to replace the inline DDL approach

**Analytics `WaitGroup` blocks process exit:**
- Current capacity: Each command fires at most 2 analytics events; 3-second HTTP timeout per event
- Limit: If the LD analytics endpoint is slow or unreachable, `analyticsClient.Wait()` in `cmd/root.go:336–351` will delay process exit by up to 3 seconds per event
- Scaling path: Cap total wait time with a context deadline

---

## Dependencies at Risk

**`go-server-sdk/v7` `subsystems` package used for FDv2 wire format:**
- Risk: `internal/dev_server/sdk/fdv2.go` imports `github.com/launchdarkly/go-server-sdk/v7/subsystems` to access `RawEvent`, `PollingPayload`, `ServerIntent`, and `IntentCode` types. These are internal SDK subsystem types not guaranteed to be stable across minor SDK versions.
- Impact: Any SDK upgrade that refactors `subsystems/` would require updating the FDv2 implementation
- Migration plan: Own the wire format types locally in `internal/dev_server/sdk/` to decouple from SDK internals

**`@launchpad-ui` internal design system pinned to specific minor versions:**
- Risk: `@launchpad-ui/components@0.4.4`, `@launchpad-ui/core@0.49.22` are internal LD packages. There is no automated upgrade mechanism and versions may fall behind LD design system changes.
- Impact: UI may look outdated or have accessibility gaps as the design system evolves
- Migration plan: Add Dependabot config for npm packages in `internal/dev_server/ui/`; currently only Go modules have Dependabot coverage (`.github/dependabot.yml`)

**`golangci-lint` version pinned in `.pre-commit-config.yaml` but not in CI:**
- Risk: `.pre-commit-config.yaml` pins `golangci-lint` at `v1.63.4` for local pre-commit. The main CI workflow (`go.yml`) runs `pre-commit/action@2c7b3805fd2a0fd8c1884dcaebf91fc102a13ecd` which uses the pre-commit config, so they match. However, `golangci-lint v1.63.4` does not support Go 1.24+ features and is already ~2 major patch versions behind.
- Impact: New Go language features or linter rules for newer Go versions are not enforced
- Migration plan: Upgrade golangci-lint to v1.64+ in `.pre-commit-config.yaml`

**`mattn/go-sqlite3` (CGO) as a hard build requirement:**
- Risk: Every build of `ldcli` requires a CGO-capable C compiler. The `make build` Makefile target does not set `CGO_ENABLED=1` explicitly, relying on the system default. On macOS with Xcode Command Line Tools, this works. On minimal CI runners or Docker-based builds without a C toolchain, the build silently fails or produces a binary without SQLite.
- Files: `Makefile:4`, `.goreleaser.yaml:14`
- Impact: Local developer builds on systems without gcc/clang will fail with an unhelpful `cgo: C compiler "gcc" not found` error
- Migration plan: Document CGO requirement prominently in README/CONTRIBUTING; alternatively evaluate `modernc.org/sqlite` (pure Go, no CGO)

---

## Missing Critical Features

**No coverage enforcement:**
- Problem: `go test ./...` runs all tests but there is no enforced coverage gate in CI. The CI workflow (`go.yml`) runs `go test ./...` without `-cover` or a threshold check.
- Blocks: There is no automated signal when new code is added without tests
- Files: `.github/workflows/go.yml:29`

**No integration or E2E test suite:**
- Problem: There are no tests that exercise the running `ldcli` binary against a real or stubbed LD API. The highest-level tests use `cmd.CallCmd` which builds the Cobra tree in-process but does not test the actual compiled binary, binary distribution, or version injection.
- Blocks: Regressions in CLI output formatting, exit codes, or flag parsing are caught only by the narrow `cmd.CallCmd` suite

**Dev server has no authentication on management API:**
- Problem: The `/dev/*` management routes (add project, upsert override, delete project) require no authentication. Anyone on the same network can read and modify flag overrides if the dev server is reachable (see Security section above: it binds to `0.0.0.0`).
- Files: `internal/dev_server/dev_server.go:86–101`, `internal/dev_server/api/server.gen.go`

---

## Test Coverage Gaps

**`cmd/login/` — login command not tested at command layer:**
- What's not tested: Full `login` command lifecycle: device auth request, browser open, token polling loop, config file write, error cases (denied, expired)
- Files: `cmd/login/login.go`
- Risk: A regression in the login flow (e.g., Viper not populated before config write) would not be caught
- Priority: High — login is a critical onboarding path

**`cmd/quickstart.go` and `internal/quickstart/` — TUI untested:**
- What's not tested: All Bubbletea model state transitions in the quickstart wizard; SDK selection; flag toggle; SDK instruction rendering
- Files: `cmd/quickstart.go`, `internal/quickstart/container.go`, `choose_sdk.go`, `show_sdk_instructions.go`
- Risk: Silent regressions in multi-step wizard flow
- Priority: Medium

**`cmd/resources/resource_cmds_test.go` — one test skipped:**
- What's not tested: `TestCreateTeam / with valid flags calls makeRequest function` is permanently skipped (`t.Skip("TODO: add back when mock client is added")`)
- Files: `cmd/resources/resource_cmds_test.go:59–60`
- Risk: The generated resource command invocation path is not exercised; regressions in how generated commands call `MakeRequest` are not caught
- Priority: Medium

**`internal/dev_server/ui/` — entire React application has 1 test:**
- What's not tested: `Flags.tsx`, `FlagsPage.tsx`, `Flag.tsx`, `ProjectEditor.tsx`, `EnvironmentSelector.tsx`, `Sync.tsx`, `api.ts`, routing, event handling
- Files: `internal/dev_server/ui/src/__tests__/` (contains only `SubmitButton.test.tsx`)
- Risk: Any refactor of the flag override UI has no regression safety net
- Priority: Medium

**`cmd/dev_server/` — dev server command layer thinly tested:**
- What's not tested: CORS flag behavior, port conflict handling, initial project settings propagation from CLI flags to `ServerParams`
- Files: `cmd/dev_server/start_server.go`, `cmd/dev_server/dev_server.go`
- Risk: Changes to how `--cors-origin` or `--port` flags are wired to `ServerParams` would not be caught
- Priority: Low

---

*Concerns audit: 2026-05-11*
