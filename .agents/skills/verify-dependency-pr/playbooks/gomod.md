# Playbook: Go modules (`go.mod` / `go.sum`)

## Baseline (from `verify.sh`)

`go-mod-tidy`, `go-directive`, `go-build-vet`, `go-test`, `go-generate-drift`, `binary-smoke`, `cli-help-diff`. With `--profile full` you also get `release-snapshot` (goreleaser-cross, needs Docker), `govulncheck`, and `golangci-lint`.

Gaps these cover that PR CI does not: tidy cleanliness, codegen drift and whether the regenerated code compiles (the #720 class), a dev-server start, help-text changes, and, with the full profile, every CGO release target.

## Impact analysis

- **Is it direct?** `.classification.updates[].direct`. For an indirect bump, find who pulls it in with `go mod why -m <module>` (run in `work/pr`).
- **Where it's used:** `rg -l '"<module>' --glob '*.go'`, plus `go list -deps ./... | rg '^<module>'`.
- **The go directive:** if `go-directive` fails, the new version needs a newer Go. CI follows go.mod (`go-version-file`), golangci-lint is pinned at v1.63.4 in `.pre-commit-config.yaml`, and the goreleaser-cross image is pinned by digest. All three must support the new Go.
- **Stale branch:** in a conflicting or old PR, look for transitive pins that would now be downgrades (`downgrades` check). A rebase usually fixes this.

## Area guide (tags from `risk-map.json`)

| Tag / module | What to check | Generated-check ideas |
|---|---|---|
| `codegen`: oapi-codegen, kin-openapi, strcase | The generator and its runtime must move together (`oapi-codegen/runtime`). `go-generate-drift` must not get worse than on base. | Regenerate in `work/pr`, build, and run `go test ./internal/dev_server/api/...`. Diff the generated command list (`ldcli __complete ""`). |
| `mocks`: go.uber.org/mock | The mocks are committed. A regen diff means the mock format changed. | Regenerate the mocks and run `go test ./internal/dev_server/...`. |
| `cgo`, `dev-server`: mattn/go-sqlite3 | CGO; the release cross-compiles (run `--profile full` with Docker). The dev server keeps state in SQLite: `internal/dev_server/db`, `events_db`, `db/backup`. | A throwaway test in `internal/dev_server/db` that covers the changed driver behavior (prepared statements, backups, type mapping) and is written to fail on the old version. Backup/restore round-trip through `db/backup`. |
| `ld-sdk`: go-server-sdk, go-sdk-common, eval | The dev server proxies SDK streaming and evaluation (`internal/dev_server/sdk`, `adapters`). | Start the dev server, add a project from a fixture (`dev-server import-project`), and evaluate through `/sdk/...` endpoints. |
| `setup`: sdk-meta | Feeds the SDK lists and snippets used by `setup`/quickstart. | Compare the SDK list output between base and PR. Check that every SDK ID referenced in `internal/` and `cmd/` still exists. |
| `cli-surface`: cobra, pflag, viper, mapstructure | Flag parsing, `LD_*` env precedence, usage templates. `cli-help-diff` shows rendering changes. | Config precedence: flag beats `LD_*` env, which beats the config file, for `--base-uri`. Run `cmd/...` tests with `-count=1`. |
| `tui`: charmbracelet/* | Interactive flows have little test coverage. | Non-TTY output of `setup`/`quickstart` help; `go test ./internal/quickstart/... ./internal/setup/...`. |
| api-client-go | Generated resource commands call it. | `go test ./cmd/resources/... ./internal/resources/...`; regenerate and diff. |

## Common verdict notes

- A patch bump with green baseline, low tier, and no reach into ldcli's code paths is "safe to merge".
- Medium tier (for example sqlite, SDKs, cobra) needs at least one proven discriminating check and an impact review.
- A pre-existing `go-generate-drift` (from #720) is not the PR's fault. Mention it once and don't let it block.
