# `ldcli explain` — agent-discoverable command schema

**Status:** Draft / proposal — see PR for discussion.
**Owner:** _agent-bench experiment_
**Audience:** LLM agents that drive `ldcli` via shell tool-use, and the humans who maintain `ldcli`.

## Problem

LLM agents using `ldcli` from a shell-tool harness spend an excessive number of
turns on "what does this command want?" discovery. Concretely, from the
`ld-agent-bench` v3.0 run against `claude-opus-4-7` (45 cells, 15 tasks ×
3 providers):

| Task | Description                                                           | `claude-cli-md` tool calls | `claude-mcp` tool calls |
|------|-----------------------------------------------------------------------|---------------------------:|------------------------:|
| T06  | Add a percentage-rollout targeting rule via semantic patch            |                     **18** |                       2 |
| T08  | Add individual targets to a variation                                 |                     **12** |                       2 |
| T15  | Update flag fallthrough variation                                     |                     **10** |                       2 |

In every cell the cost pattern was the same: the model invoked
`ldcli flags update --help`, scanned the (~10 KB) help text for the
`--data` flag, guessed a JSON body, got a 400, re-read `--help`, guessed
again. The MCP version finished the same task in two calls because the
tool's input schema is typed.

The CLI doesn't need MCP — it needs a way to surface the same typed schema
*through* the CLI. That's the proposal here.

## Proposal

Add a top-level subcommand:

```
ldcli explain <command-path> [--json|--markdown] [--list]
```

`explain` takes a command path (positional args; `ldcli` prefix optional) and
emits a structured description in one call. It never talks to the API.

- `--json` (default): canonical machine-readable shape. Agents should always
  use this.
- `--markdown`: pretty-printed for humans on a TTY.
- `--list`: list the commands that currently have a curated explanation.

### JSON output shape

The full Go types live in `internal/explain/types.go`. The on-the-wire JSON
shape is, abbreviated:

```jsonc
{
  "command":     "ldcli flags update",
  "summary":     "...",
  "description": "...",
  "stability":   "stable",
  "httpMethod":  "PATCH",
  "path":        "/api/v2/flags/{projectKey}/{featureFlagKey}",
  "operationId": "patchFeatureFlag",

  "inputs": [
    {
      "name": "data", "location": "flag", "type": "object", "required": true,
      "fields": [
        {"name": "environmentKey", "type": "string"},
        {"name": "instructions", "type": "array", "required": true,
         "fields": [{
            "name": "[]", "type": "object",
            "fields": [{"name": "kind", "type": "string", "required": true, "enum": [...]}],
            "oneOf": [
              {"name": "addRule", "type": "object", "fields": [...]},
              {"name": "removeRule", "type": "object", "fields": [...]},
              {"name": "addTargets", "type": "object", "fields": [...]}
              // ...
            ]
         }]}
      ]
    }
  ],

  "output": {
    "format": "json",
    "type": "object",
    "fields": [{"name": "key"}, {"name": "_version"}, {"name": "environments"}, ...]
  },

  "errors": [
    {"code": "approval_required", "httpStatus": 405,
     "description": "...", "remediation": "..."}
  ],

  "examples": [
    {
      "title": "Add a percentage-rollout targeting rule",
      "args":  ["flags","update","--project-key","default","--feature-flag-key","new-checkout","--semantic-patch","--data","@-"],
      "body":  "{...}",
      "result": "Returns the updated flag JSON..."
    }
  ],

  "agentNotes": [
    "Prefer --semantic-patch over raw JSON patch...",
    "Use --dry-run first when constructing a non-trivial patch..."
  ],
  "seeAlso": ["ldcli flags get", "ldcli flags toggle-on"]
}
```

A concrete sample is checked in at
`internal/explain/testdata/flags_update.json` and refreshed by the snapshot
test (run with `UPDATE_GOLDEN=1`).

### Composition with existing agent-forward flags

`explain` is purely informational; it makes zero API calls and respects no
runtime state. It composes cleanly with the post-v3 agent-forward surface:

- `--fields` — still applies on real write/read commands; `explain` documents
  which output fields are available so the agent can pick.
- `--dry-run` — `explain` lists `--dry-run` in `inputs[]` when supported, and
  `agentNotes` recommends using it for non-trivial patches.
- `--json` / `--output` — `explain` is a sibling, not a replacement; the
  recommended workflow is `explain` → construct payload → `<command> --json
  --dry-run` → `<command> --json`.

## Implementation

This PR ships the skeleton plus two real coverages.

```
cmd/explain/                  # top-level command
  explain.go                  # Cobra wiring, JSON/markdown rendering
  explain_test.go             # output-shape + error-path tests
internal/explain/             # package
  types.go                    # CommandExplanation, InputSpec, OutputSpec, ...
  registry.go                 # Registry + DefaultRegistry
  errors.go                   # ErrCommandNotFound
  render.go                   # RenderJSON / RenderMarkdown
  flags_update.go             # curated: ldcli flags update (semantic-patch)
  flags_list.go               # curated: ldcli flags list (filter grammar)
  testdata/flags_update.json  # golden snapshot
  explain_test.go             # registry + snapshot tests
```

`cmd/root.go` adds `explain` to the list of commands that bypass the global
`--access-token` requirement (it's read-only and offline) and wires
`explain.DefaultRegistry()` into the root.

## Path to full coverage

This PR covers two commands end-to-end. To reach the rest of the surface
without growing the codebase by 50 KB of hand-rolled metadata, we propose:

### 1. Auto-generated resource commands (~140 commands)

The generated `cmd/resources/resource_cmds.go` already encodes
`OperationData` for every operation (HTTP method, path, params, request-body
required-ness, semantic-patch support). The remaining schema lives in
`ld-openapi.json`. Add an `OpenAPIExplainer` to the registry as a fallback:

- Resolve the Cobra command to its `OperationData.OperationID`.
- Pull the request schema (`#/components/schemas/...`) for `--data`,
  flatten one level of `$ref`s so agents see real fields not pointers.
- Pull the success response schema; carry forward `description` so agents
  understand what fields they're projecting with `--fields`.
- Pull the error responses; map each HTTP status to an `ErrorSpec`. The
  LaunchDarkly API uses a consistent `{code, message}` error envelope that
  we can hard-code as the default shape.

This is the single biggest unlock and is mostly mechanical. Estimated effort:
~1 week of work; most of it is teaching the loader to dereference `$ref`s
without blowing up on cyclic schemas.

### 2. Hand-rolled commands (~5 commands today)

For commands like `flags toggle-on`, `flags archive`, `members invite`,
`dev-server ...` — anything not derived from OpenAPI — expose an
`Explain()` hook on the Cobra command:

```go
type Explainable interface {
    Explain() explain.CommandExplanation
}
```

The registry checks for this interface before falling back to OpenAPI or
the flag tree.

### 3. Flag-tree fallback (any Cobra command)

For commands without curated or OpenAPI metadata, produce a best-effort
explanation by walking the Cobra flag tree:

- One `InputSpec` per `flag.Flag`, with `name`, `type` (from `Flag.Value.Type()`),
  `description` (from `Flag.Usage`), `default` (from `Flag.DefValue`),
  `required` (from the `required` annotation).
- No example, no error catalog, but at least agents stop seeing a "command
  not found" from `explain` for commands ldcli ships.

### 4. Curated examples are always hand-written

The OpenAPI spec doesn't carry compelling, agent-friendly examples. We
recommend a `cmd/<name>/examples.go` convention: each package that wants
to override or supplement the auto-generated explanation owns its examples,
and they're registered via the `Explainable` interface above. The bar is
1–3 examples per command, the most common ones the bench harness exercises.

## Agent UX

`AGENTS.md` is updated in this PR with one paragraph telling agents to call
`ldcli explain <cmd>` before constructing complex payloads. We expect the
bench harness ("claude-cli-explain" provider; see PR description) to
demonstrate the win quantitatively before we commit to full coverage.

## Open questions

1. **Should `explain` be the default for `--help` on agent-context invocations?**
   When `LD_AGENT_CONTEXT` is set (or the process detects it's not a TTY and
   the analytics `agent` annotation is on), we could route `<cmd> --help` to
   `explain <cmd>` automatically. This is a behavior change that needs Ramon's
   eyes.
2. **Schema versioning.** The JSON shape should be versioned (e.g.
   `"_schemaVersion": "1"`) so agents can fail fast on a breaking change. Not
   included in this draft pending discussion.
3. **Coverage as CI gate.** Once OpenAPIExplainer lands, we can fail CI if a
   new operation ships without at least a fallback. Worth doing.
4. **Examples in OpenAPI.** Upstream-ing curated agent examples into the
   OpenAPI spec itself (under `x-ldcli-examples`) would let us derive them
   instead of hand-writing in Go. Wide-reaching change; not in scope here.

## References

- Bench harness: `ld-agent-bench` (T06 / T08 / T15 specifically)
- Prior art: PR #660 — agent-forward CLI (Ramon)
- LaunchDarkly REST API:
  https://launchdarkly.com/docs/api/feature-flags/patch-feature-flag
- CLI House Style guide §4 (examples), §16 (`--dry-run`), §19 (doctor/whoami)
