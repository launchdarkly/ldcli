# Validating a LaunchDarkly REST API version upgrade

Use this when a change bumps `github.com/launchdarkly/api-client-go` or changes the `LD-API-Version` header ldcli sends. The worked example is [PR 832](https://github.com/launchdarkly/ldcli/pull/832) (api-client-go v14 to v24, header `20240415`, [REL-16083](https://launchdarkly.atlassian.net/browse/REL-16083)).

A green `go test ./...` does not prove the upgrade. The header tests talk to `httptest`. They stay green if a generated command sends the wrong version, if a live response no longer unmarshals, or if login starts sending a version header the device-authorization endpoints do not expect.

## Two clients, one version

ldcli talks to the REST API through two stacks. Pinning one and leaving the other on the access token's default version is the failure mode this playbook exists to catch.

| Stack | Code | Who uses it |
| --- | --- | --- |
| Typed client | `internal/client.New` builds `api-client-go` | setup, quickstart, dev-server flag and environment sync, and the hand-written flags, members, projects, and environments helpers |
| Resources client | `internal/resources.ResourcesClient.MakeRequest` | generated commands in `cmd/resources/resource_cmds.go`, plus hand-written commands that call this client (`flags toggle-on` / `toggle-off`, `flags archive`, `whoami`, `members invite`, `sdk-active`, sourcemaps and symbols upload, setup verify) |

`api-client-go` v21 and later add `LD-API-Version: 20240415` in `prepareRequest` when the caller did not set a version. The resources client does not use that library. It only sends the header if `MakeRequest` sets it.

`MakeUnauthenticatedRequest` calls `MakeRequest` with `isBeta` false. Login (`/internal/device-authorization` and `/internal/device-authorization/token`) and the dev-server commands that use the unauthenticated client therefore send the same stable header as every other non-beta call.

## Before editing

1. Name the target version string (today `20240415`) and the `api-client-go` major that emits it by default. v21 is the first major with that default. Later majors are fine when they match another LaunchDarkly Go consumer (v24 matches the Terraform provider) and the changelog's breaking changes are accounted for below.
2. Diff the client changelog between the old major and the new one. Record every constructor, field, or required response field ldcli touches. v24 dropped the value argument from `NewPatchOperation`. `internal/flags/client.go` builds `PatchOperation{Op, Path, Value}` directly. v24 models also reject responses that omit a spec-required field.
3. Search `ld-openapi.json` for operations whose `LD-API-Version` parameter is required and whose enum does not include the target version. The generator's beta switch is only `strings.Contains(tag, "(beta)")` in `cmd/resources/resources.go`. A tag without `(beta)` is generated as `IsBeta: false` even when the spec allows only `beta`.
4. Confirm `cmd/resources/resources.go` `makeRequest` still copies only `path` and `query` parameters. The generated `--ld-api-version` flag is not written onto the request. `IsBeta` is the only switch `MakeRequest` honors.

As of PR 832, the only operations that fail step 3 are the five `ai-configs` agent-graph commands (`list-agent-graphs`, `get-agent-graph`, `create-agent-graph`, `update-agent-graph`, `delete-agent-graph`). Their tag is `AI Configs`. Their header enum is `["beta"]`. They are generated with `IsBeta: false`, so after the pin they send `20240415`. They sent no version header before the pin. Both shapes fail a beta-only route. Fix them in the generator (set `IsBeta` when the tag contains `(beta)` or when the header enum is only `beta`), then regenerate `cmd/resources/resource_cmds.go`. Do not special-case paths inside `MakeRequest`.

## Code checklist

- [ ] `go.mod` requires the new `github.com/launchdarkly/api-client-go/vN` module, and no `.go` file still imports the previous major.
- [ ] `internal/resources` keeps a single version constant and `MakeRequest` sets `LD-API-Version` to that constant when `isBeta` is false and to `beta` when `isBeta` is true.
- [ ] Every `api-client-go` call site compiles against the new models. Pay attention to removed constructors and to fields that became required on responses.
- [ ] Flag patches still serialize `op`, `path`, and `value`. A bool `false` must remain in the JSON. `PatchOperation` omits a nil `Value`.
- [ ] Generated commands whose OpenAPI tag contains `(beta)` still pass `IsBeta: true`.
- [ ] Generated commands whose `LD-API-Version` enum is only `beta` also pass `IsBeta: true`, even when the tag has no `(beta)`.
- [ ] The PR describes behavior that changes for tokens whose default version is older than the pin: list page size, omitted `environments` on `flags list` unless `filterEnv` is set, and filters the new version rejects with 400.

## Tests that have to fail if the pin is wrong

These are the tests to add or extend. A unit test that passes `isBeta` in directly does not cover the generator.

| What to assert | Where |
| --- | --- |
| Typed client sends `LD-API-Version: <target>` on a real method call such as `ProjectsApi.GetProjects` | `internal/client/client_test.go` |
| `MakeRequest` sends `<target>` when `isBeta` is false and `beta` when `isBeta` is true | `internal/resources/client_test.go` |
| A generated command with `(beta)` in its tag calls `MakeRequest` with `isBeta` true | generator test, or a test that loads one `OperationCmd` from `resource_cmds.go` |
| Each operation from the OpenAPI search in step 3 calls `MakeRequest` with `isBeta` true | same |
| A flag patch body contains `"value": false` for toggle-off | typed-client test around `FlagsClient.Update`, not the resources mock used by the quickstart toggle tests |

Run:

```bash
go test ./internal/client ./internal/resources ./internal/flags ./cmd/resources
go test ./...
```

`golangci-lint` v1.63.4 is what CI runs. `make test` is `go test ./...`.

## Live checks CI will not do

Run these against an account whose access token defaults to an older API version than the pin, and again with a token created by `ldcli login` (those tokens already default to the current version). Use `--access-token` so you are not only testing the login-token path.

- [ ] `projects list`, `flags list`, and one flag `GET` return JSON the CLI prints without an unmarshal error. v24 fails the call when a required field is missing.
- [ ] `flags list` plaintext still prints the existing paging line (`Showing results 1 - 20 of N`) when the project has more flags than the new default page size.
- [ ] One command whose tag contains `(beta)` sends `LD-API-Version: beta`. Capture it with a proxy or with `--base-uri` pointed at a local recorder.
- [ ] `ai-configs list-agent-graphs` sends `beta`, not the stable version. Until the generator fix lands, this check fails and should be called out in the PR rather than treated as covered by the header unit tests.
- [ ] `ldcli login` still completes device authorization. That path is unauthenticated and now sends the stable version header.
- [ ] Dev-server project sync still loads flags. It pages `GetFeatureFlags` and reads `items` and `variations`.

## What to write in the pull request

- The target version, the client major, and why that major (minimum with the default header, or a later major aligned with another consumer).
- Which of the two clients changed.
- Breaking model changes and the call sites updated for them.
- Operations that require `beta` and whether they still send it.
- User-visible response changes for older tokens (page size, dropped fields, filters that now return 400).
- Commands you did not exercise against a live account. The PR template's "validated against all supported platform versions" box stays unchecked until the live checks above are done.
