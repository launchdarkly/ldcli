# Validating a LaunchDarkly REST API version upgrade

Use this when a change bumps `github.com/launchdarkly/api-client-go` or changes the `LD-API-Version` header ldcli sends.

A green `go test ./...` does not prove the upgrade. The header tests talk to `httptest`. They stay green if a generated command sends the wrong version, if a live response no longer unmarshals, or if login starts sending a version header the device-authorization endpoints do not expect.

## Two clients, one version

ldcli talks to the REST API through two stacks. Pinning one and leaving the other on the access token's default version is the failure mode this playbook exists to catch.

| Stack | Entry point | Find its callers |
| --- | --- | --- |
| Typed client | `internal/client.New`, which builds an `api-client-go` client | `rg -n 'client\.New\(' --glob '*.go' --glob '!*_test.go' cmd internal` |
| Resources client | `internal/resources.ResourcesClient.MakeRequest` | `rg -l 'MakeRequest\(\|MakeUnauthenticatedRequest\(' --glob '*.go' --glob '!*_test.go' cmd internal` |

The resources client serves every generated command in `cmd/resources/resource_cmds.go` and the hand-written commands those searches list. It does not use `api-client-go`, so it sends `LD-API-Version` only when `MakeRequest` sets it.

`MakeUnauthenticatedRequest` calls `MakeRequest` with `isBeta` false. Login (`/internal/device-authorization` and `/internal/device-authorization/token`) and the dev-server commands that use the unauthenticated client get whatever header `MakeRequest` sends for non-beta calls.

## Before editing

1. Name the target version string. The spec's version changelog is in `ld-openapi.json` under `info.description`, in the "API version changelog" table. It also lists what each version changes for callers on older versions.
2. Find the `api-client-go` major whose `prepareRequest` adds that version when the caller sets none. After `go get github.com/launchdarkly/api-client-go/vN`, check:

   ```bash
   rg -n 'Header.Add\("LD-API-Version"' "$(go list -m -f '{{.Dir}}' github.com/launchdarkly/api-client-go/vN)/client.go"
   ```

   Choose the lowest major that sends the target, or a later one when there is a reason, such as matching another LaunchDarkly Go consumer. Write the reason in the PR.
3. Read the client's release notes between the old major and the new one. Record every constructor, field, or required response field ldcli uses. Majors can remove constructor arguments and make response fields required, and generated models then reject responses missing those fields.
4. List the operations whose `LD-API-Version` header is required, whose enum does not contain the target version, and whose tag lacks `(beta)`:

   ```bash
   jq -r --arg target 20240415 '
     .paths | to_entries[] | .key as $path | .value | to_entries[]
     | select(.value | type == "object" and has("operationId"))
     | .key as $method | .value as $op
     | ($op.parameters // [])[]
     | select(.in == "header" and .name == "LD-API-Version" and .required == true)
     | select((.schema.enum // []) | index($target) | not)
     | select([$op.tags[]? | contains("(beta)")] | any | not)
     | "\($op.operationId)\t\($method | ascii_upcase) \($path)\t\(.schema.enum)"
   ' ld-openapi.json
   ```

   Replace `20240415` with the target. Every operation this prints needs `IsBeta: true` in `cmd/resources/resource_cmds.go`. If the generator in `cmd/resources/resources.go` sets `IsBeta` only from a `(beta)` tag, these operations come out `false`. Fix them in the generator and regenerate. Do not special-case paths inside `MakeRequest`.
5. Check whether `makeRequest` in `cmd/resources/resources.go` copies header parameters onto the request. If it copies only `path` and `query`, the generated `--ld-api-version` flag has no effect, and `IsBeta` is the only switch `MakeRequest` honors.

## Code checklist

- [ ] `go.mod` requires the new `github.com/launchdarkly/api-client-go/vN` module. `rg 'api-client-go/v' --glob '*.go'` shows only the new major.
- [ ] `MakeRequest` sets `LD-API-Version` from one constant when `isBeta` is false, and to `beta` when `isBeta` is true.
- [ ] Every `api-client-go` call site compiles against the new models, and you have checked each breaking change recorded in step 3.
- [ ] Flag patches still serialize `op`, `path`, and `value`. A bool `false` must remain in the JSON, because `PatchOperation` omits a nil `Value`.
- [ ] Generated commands whose OpenAPI tag contains `(beta)` pass `IsBeta: true`.
- [ ] Every operation from the spec query in step 4 passes `IsBeta: true`.
- [ ] The PR lists what changes for tokens whose default version is older than the target, taken from the spec's version changelog: default page sizes, fields that are omitted, and filters or parameters that now return 400.

## Tests that have to fail if the pin is wrong

Add or extend these. A unit test that passes `isBeta` in directly does not cover the generator.

| What to assert | Where |
| --- | --- |
| Typed client sends the target version on a real method call such as `ProjectsApi.GetProjects` | `internal/client/client_test.go` |
| `MakeRequest` sends the target version when `isBeta` is false and `beta` when `isBeta` is true | `internal/resources/client_test.go` |
| A generated command with `(beta)` in its tag calls `MakeRequest` with `isBeta` true | generator test, or a test that loads one `OperationCmd` from `resource_cmds.go` |
| Every operation from the step 4 spec query calls `MakeRequest` with `isBeta` true | same |
| The PATCH body sent by `FlagsClient.Update` contains `"value": false` for toggle-off | typed-client test that reads the request body |

Run:

```bash
go test ./internal/client ./internal/resources ./internal/flags ./cmd/resources
go test ./...
```

CI also runs pre-commit, which runs `golangci-lint` at the `rev` pinned in `.pre-commit-config.yaml`.

## Live checks CI will not do

Run these with a token whose default version is older than the target, and again with a token created by `ldcli login`. Pass the older token with `--access-token` so you are not testing only the login-token path. To see headers, point `--base-uri` at a local recorder or run through a proxy.

- [ ] `projects list`, `flags list`, and one flag `GET` print JSON without an unmarshal error.
- [ ] `flags list` plaintext prints the paging line (`Showing results 1 - N of M`) when the project has more flags than the default page size.
- [ ] One command whose tag contains `(beta)` sends `LD-API-Version: beta`.
- [ ] One command from the step 4 spec query sends `LD-API-Version: beta`. The header unit tests do not prove this.
- [ ] `ldcli login` completes device authorization. That path is unauthenticated and sends the non-beta header from `MakeRequest`.
- [ ] Dev-server project sync loads flags. It pages `GetFeatureFlags` and reads `items` and `variations`.

## What to write in the pull request

- The target version, the client major, and why that major.
- Which of the two clients changed.
- Breaking model changes and the call sites updated for them.
- The operations from the step 4 spec query and whether each sends `beta`.
- What changes for older tokens, from the spec's version changelog.
- The live checks you ran and the ones you did not. The PR template's "validated against all supported platform versions" box stays unchecked until the live checks are done.
