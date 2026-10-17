# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Project Overview

LaunchDarkly CLI (`ldcli`) — a Go CLI for managing LaunchDarkly feature flags. Built with Cobra/Viper, distributed via Homebrew, Docker, NPM, and GitHub Releases.

## Writing Style: Simple English

Write all prose in Simple English. Simple English is plain English that follows the rules of ASD-STE100 Simplified Technical English. The full rules are in [`.agents/skills/simple-english/SKILL.md`](.agents/skills/simple-english/SKILL.md). Read that file before you write or rewrite a document.

The rules apply to this text:

- Replies to the user
- Markdown files, such as `README.md` and `CONTRIBUTING.md`
- Pull request titles and descriptions
- Commit messages
- New code comments, CLI help text, and error messages

Do not change these items:

- Code, identifiers, commands, flags, file paths, and quoted errors
- Generated files, such as `CHANGELOG.md` and `cmd/resources/resource_cmds.go`
- Text that your task does not touch

Agents break these rules most often:

1. Write short sentences. Use 20 words at most for an instruction and 25 words at most for a description.
2. Use active voice and simple tenses. Write `The command deleted the row`, not `The row has been deleted`.
3. Use `can`, `will`, or `must`. Do not use `should`, `would`, `may`, `might`, or `could`.
4. Put a condition before its command: "If the build fails, read the log."
5. Use one word for one meaning in a document. For example, use `configuration` every time, not `config` in one place and `settings` in another.
6. Define a technical term the first time that you use it.
7. Do not use contractions, semicolons, or em dashes.
8. State facts. Do not add words such as `robust`, `seamless`, or `crucial`.
9. In a reply, put the answer in the first sentence. Write prose, with no headers, bold text, lists, or tables.

### Agent Hooks

Hooks load these rules at the start of a session. They also lint the Markdown files that an agent writes. The hooks are advisory, so they never block an action. Each hook runs `.agents/skills/simple-english/scripts/hook.py`, which needs `python3`.

| Tool | Configuration | What the hooks do |
| --- | --- | --- |
| Cursor | `.cursor/hooks.json` | Load the rules at session start. Lint each Markdown file after a write. |
| Claude Code | `.claude/settings.json` | Load the rules at session start. Lint each Markdown file after a write. Report bold text, headers, lists, and em dashes in each reply. |
| Codex | `.codex/hooks.json` | Load the rules at session start. |

These limits apply:

- Cursor Cloud Agents do not run session start hooks. They get the rules from this file, and the Markdown lint still runs.
- Cursor also runs the hooks in `.claude/settings.json`. The script finds this case and runs only the Cursor hooks.
- Codex runs project hooks only in a trusted project. Codex also asks you to approve each hook. Open `/hooks` to approve it.
- The lint reports only the lines that differ from the last commit. A new file gets a full lint.
- The lint skips `CHANGELOG.md`, the skill folder, and files outside the repository.

To turn off the hooks, set `SIMPLE_ENGLISH_HOOKS=off`. To test the hooks, run `python3 .agents/skills/simple-english/scripts/test_hook.py`. The file `.agents/skills/simple-english/UPSTREAM.md` gives the source of the skill and the steps to update it.

## Common Commands

```bash
make build              # Build binary as ./ldcli
make test               # Run all tests (go test ./...)
go test ./path/to/pkg   # Run tests for a specific package
make generate           # Regenerate code from OpenAPI spec (go generate ./...)
make vendor             # go mod tidy + go mod vendor (vendor/ is not committed; usually you only want go mod tidy)
make install-hooks      # Install git pre-commit hooks
make openapi-spec-update # Download latest OpenAPI spec and regenerate code
```

## Code Generation

Resource commands are auto-generated from the LaunchDarkly OpenAPI spec (`ld-openapi.json`):

- **Generator:** `cmd/resources/gen_resources.go` (build tag: `gen_resources`)
- **Template:** `cmd/resources/resource_cmds.tmpl`
- **Output:** `cmd/resources/resource_cmds.go` (~613KB, do not edit manually)
- **Trigger:** `//go:generate` directive in `cmd/root.go`

The dev server API is also generated: `internal/dev_server/api/server.gen.go` (via oapi-codegen).

## Architecture

**Entry point:** `main.go` → `cmd.Execute(version)` → `cmd/root.go` (Cobra root command)

**Command layer (`cmd/`):**
- Each subcommand (flags, members, config, login, dev-server, sourcemaps, resources) has its own package
- Resource commands are generated; custom commands are hand-written
- Analytics tracking via `PersistentPreRun` hooks

**Internal packages (`internal/`):**
- Each domain package (flags, environments, members, projects, resources, dev_server) exposes a `Client` interface for dependency injection
- `internal/dev_server/` — local dev server with SQLite storage, embedded React UI, and LaunchDarkly SDK integration
- `internal/config/` — manages CLI configuration via `$XDG_CONFIG_HOME/ldcli/config.yml`
- `internal/output/` — response formatting (JSON/plaintext)

**Configuration precedence:** CLI flags → environment variables (prefix `LD_`) → config file

## Adding a New Command

1. Add command to root via `cmd.AddCommand` in `NewRootCommand()` in `cmd/root.go`
2. Update usage template in `getUsageTemplate()` in `cmd/root.go`
3. Add analytics instrumentation via `PersistentPreRun` calling `tracker.SendCommandRunEvent`

## Dev Server Frontend

Located at `internal/dev_server/ui/` — React 18 + TypeScript + Vite, embedded into the Go binary.

```bash
cd internal/dev_server/ui
npm ci
npm test        # Vitest
npm run lint    # ESLint
npm run build   # Production build (checked into repo)



```

## Testing

- Go tests use `testify` for assertions and `go.uber.org/mock` for mocking
- Mock generation via `mockgen`
- Test data in `cmd/resources/test_data/` and `cmd/config/testdata/`
- `LD_*` environment variables and `~/.config/ldcli/config.yml` leak into `cmd/` tests and make some of them fail (e.g. `cmd/setup`, `cmd/whoami`). Run tests with `LD_*` unset and `XDG_CONFIG_HOME` pointed at an empty directory.

## Pre-commit Hooks

Installed via `make install-hooks`. `.pre-commit-config.yaml` runs only:
- `golangci-lint` v1.63.4 (default linters, no `.golangci.yml`)
- `end-of-file-fixer`

It does **not** check `go fmt`, `go mod tidy`, or the UI build. Run `go mod tidy` yourself; `scripts/dependency-pr/verify.sh` checks tidiness, codegen drift, and UI `dist/` drift.

## Dependency PR Verification

To verify a Dependabot PR, follow the `verify-dependency-pr` skill (`.agents/skills/verify-dependency-pr/SKILL.md`). The skill holds the verification to the standard of a diligent reviewer, so that a person only answers specific decisions.

- Run `scripts/dependency-pr/verify.sh --pr <N>`, or `make verify-dependency-pr ARGS="--pr <N>"`.
- The script writes `.verify-out/pr-<N>/result.json` and `comment.md`. It does not post, approve, or merge.
- The verdict is `safe-to-merge`, `needs-human` (with one question for each decision), `block` (with the fix), or `incomplete` (a check did not run).
- The caller runs `post-comment.sh` to post the comment.
- Unit tests: `scripts/dependency-pr/test/run.sh`. Replays of past PRs (needs network): `scripts/dependency-pr/test/replay.sh`.

## Linting

- Go: `golangci-lint` (v1.63.4) via pre-commit
- Frontend: ESLint + Prettier
