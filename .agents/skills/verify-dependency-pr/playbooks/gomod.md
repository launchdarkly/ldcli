# Playbook: Go modules (`go.mod` and `go.sum`)

## Diligent reviewer standard

A diligent reviewer makes sure that these statements are true for a Go update:

1. ldcli builds, passes `go vet`, and passes its tests with the new version.
2. `go mod tidy` and `go generate ./...` make no change. If a generator changed, the regenerated code builds.
3. The binary runs. All commands print help, and the dev server serves its UI and API.
4. Every release target compiles. The release uses CGO for SQLite on linux (musl, static), windows (mingw), and macOS (osxcross).
5. The upstream changes between the two versions are known. Each breaking change, security fix, and behavior change is mapped to ldcli code, or is shown to be not reachable.
6. Each new or changed module that goes into the binary is known, and its license is in the policy.
7. A go or toolchain directive change works with every tool that CI and the release use.
8. No new reachable vulnerability appears.

## What the baseline checks

| Statement | Check |
|---|---|
| 1 | `go-build-vet`, `go-test` (both are gates) |
| 2 | `go-mod-tidy`, `go-generate-drift` |
| 3 | `binary-smoke` (gate), `cli-help-diff` |
| 4 | `release-snapshot` (full profile, needs Docker). It is a gate for updates with the `cgo` or `go-directive` tag. If the private release image cannot be pulled, the check uses the public `goreleaser/goreleaser-cross:v1.24.2` image and musl.cc toolchains, both pinned. The summary names the image, and the details list the fidelity gaps. |
| 5 | `upstream-changes` collects the notes and the compare link. For `golang.org/x/*` modules, it uses the `github.com/golang` mirror, and the notes are the commit messages. The agent does the mapping. |
| 6 | `transitive-changes`, `license-changes` |
| 7 | `go-directive`, plus `golangci-lint` and `release-snapshot` as gates when the tag `go-directive` is set |
| 8 | `govulncheck`. Only a new reachable advisory fails. The details list the advisories that the PR fixes (reachable, in an imported package, or in a required module) and the reachable advisories that stay. |

## What the agent must do

- Read the notes in `state/checks/upstream-changes/pr/notes/`. If a module has no notes, read the compare diff. Look at the API changes in the packages that ldcli imports.
- Find the ldcli code that uses the module: `rg -l '"<module>' --glob '*.go'`. For an indirect update, find the reason with `go mod why -m <module>` in `work/pr`.
- For each changed transitive module in the `transitive-changes` details, find what pulls it in, and say if it changes behavior that ldcli uses.
- If a directive change appears, read the release notes of the new Go version for changes that affect ldcli.

## Risk areas

| Tag or module | What to look at | Generated-check ideas |
|---|---|---|
| `codegen`: oapi-codegen, kin-openapi, strcase | The generator and its runtime library must change together (for example `oapi-codegen/runtime`). `go-generate-drift` must not become worse than on base. | Regenerate in `work/pr`, build, and run `go test ./internal/dev_server/api/...`. |
| `mocks`: go.uber.org/mock | The repository commits the mocks. A diff after regeneration means that the mock format changed. | Regenerate the mocks and run `go test ./internal/dev_server/...`. |
| `cgo`, `dev-server`: mattn/go-sqlite3 | The dev server stores its state in SQLite: `internal/dev_server/db`, `events_db`, `db/backup`. | A temporary test in the affected package. For example, the #829 check measures allocations per row in `events_db.QueryEvents`. |
| `ld-sdk`: go-server-sdk, go-sdk-common | The dev server forwards SDK streams and evaluations (`internal/dev_server/sdk`, `adapters`). | Start the dev server, import a project from a fixture, and evaluate a flag through the `/sdk` endpoints. |
| `setup`: sdk-meta | It supplies the SDK lists for `setup` and quickstart. | Compare the SDK list output on base and PR. Make sure that each SDK ID that ldcli uses still exists. |
| `cli-surface`: cobra, pflag, viper, mapstructure | Flag parsing, the order of `LD_*` variables and the configuration file, and the usage templates. `cli-help-diff` shows the help changes. | A test of the configuration order for `--base-uri`: the flag first, then `LD_*`, then the file. |
| `tui`: charmbracelet | The interactive flows have few tests. | Help output and output without a terminal for `setup` and quickstart. |

## When to ask a person

- `cli-help-diff` shows a change in help text or flags. Ask if the change is acceptable.
- A new module or a changed module has a license outside the policy, or its license changed.
- The impact review finds a behavior change that ldcli users will see. Ask if the change is acceptable, and state the change.

A pre-existing `go-generate-drift` failure (from #720) is not caused by the PR. The comment lists it once. It does not block the PR.
