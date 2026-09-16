---
name: validate-ldcli-changes
description: >-
  Run the ldcli CLI locally and validate changes against a LaunchDarkly staging
  account before opening a PR. Use whenever you change CLI behavior — resource
  commands (flags, projects, environments, members, segments, ...), custom
  commands (setup, login, config, dev-server, sourcemaps, symbols, whoami),
  output formatting, config resolution, or auth. Produces a human-style screen
  recording of the CLI in action that must be attached to the PR.
---

# Validate ldcli changes locally and against staging

Every change that affects what a user sees or does when they run `ldcli` must be
proven by actually running the built CLI — not just by unit tests. This skill
tells you how to build the CLI, point it at **staging** (never production), pick
the validation that fits the change, and record a video of yourself driving the
CLI like a human so a reviewer can trust the change.

## When to use this skill

Use it for any change to CLI behavior, including:

- Resource commands (`flags`, `projects`, `environments`, `members`, `segments`, and the rest of the generated resource commands).
- Hand-written commands (`setup`, `login`, `config`, `dev-server`, `sourcemaps`, `symbols`, `whoami`).
- Output/formatting (`--output`, `--json`, `--fields`, plaintext/markdown, TTY defaults).
- Config precedence, flags, environment variables, or auth handling.

Skip it only for changes that cannot affect runtime behavior (pure docs, comments, test-only refactors). When in doubt, run the CLI.

## 1. Build the CLI locally

Always test the binary you just built, not an installed `ldcli` on the PATH.

```bash
make build            # produces ./ldcli in the repo root
./ldcli --version     # confirm the binary runs (prints "ldcli version dev")
```

Rebuild with `make build` after every code change, and invoke it as `./ldcli`
(with the leading `./`) so you never accidentally exercise a globally installed
copy.

## 2. Point the CLI at staging

Validate against **staging**, never production. The CLI derives every API call
from two settings — the base URI and an access token — resolved in this order:
**CLI flags → `LD_`-prefixed environment variables → config file**
(`$XDG_CONFIG_HOME/ldcli/config.yml`).

Staging base URI: `https://app.staging.launchdarkly.com`

Prefer environment variables so nothing sensitive is written to disk and so you
never mutate the developer's real config file:

```bash
export LD_BASE_URI="https://app.staging.launchdarkly.com"
export LD_ACCESS_TOKEN="<staging access token>"   # from Cursor Secrets, never hard-coded
```

Rules for credentials:

- The staging access token is a **secret**. Provide it through the Cursor
  Secrets panel as `LD_ACCESS_TOKEN` (and optionally `LD_BASE_URI`). Never paste
  a token into a command you commit, into the config file, into logs, or into the
  recorded video (see step 5).
- If `LD_ACCESS_TOKEN` is not set, staging-authenticated validation is blocked.
  Do the local, no-auth validation you can (see step 3), and tell the user in your
  summary that a staging `LD_ACCESS_TOKEN` secret is required to finish
  authenticated validation. Do not fall back to production.
- Create a staging token from the authorization page the CLI points you to:
  `https://app.staging.launchdarkly.com/settings/authorization`.

Sanity-check auth before anything else — `whoami` confirms the token and base URI
resolve to the account you expect:

```bash
./ldcli whoami --output json
```

A helpful error (`no access token configured...`) instead of identity output
means the token is missing or wrong — fix that before continuing.

## 3. Choose validation that fits the change

Run the commands that actually exercise your change. Match the change to the
validation:

| Change area | How to validate against staging |
| --- | --- |
| A resource command (e.g. `flags`, `projects`, `segments`) | Drive the real lifecycle for the affected verbs: `create` → `get`/`list` → `update` → `delete`. Confirm the API response and that the resource actually changed in staging. |
| A single verb/subcommand | Run that exact subcommand with realistic flags and payloads; also run an adjacent verb to confirm nothing regressed. |
| Output / formatting (`--output`, `--json`, `--fields`) | Run the same command with `--output json`, `--output plaintext`, and (where supported) `--output markdown`; confirm each renders correctly. Remember non-TTY defaults to JSON; force plaintext with `FORCE_TTY=1` when demonstrating the terminal default. |
| Config (`config --set/--unset/--list`) | Set, list, and unset the affected keys; confirm precedence (flag over env over file). Use env vars for tokens so you don't clobber a real config file. |
| Auth / global flags | `./ldcli whoami` for the happy path, plus the missing/invalid-token error path. |
| `dev-server` | Start the server, then exercise it (list/sync a project, add/remove an override) and confirm served flag values. |
| `setup` / `login` (interactive) | Walk the interactive flow end to end; capture each prompt and the final success state. |
| Error handling / messages | Trigger the error deliberately (bad key, missing flag, no token) and confirm the message and exit code are correct. |

Also keep the automated tests green (`make test`, and `npm test` in
`internal/dev_server/ui` for UI changes). Automated tests complement, but do not
replace, running the real CLI.

## 4. Test safely against staging

Staging is shared. Be a good tenant:

- Prefer a scratch/test project or environment. Do not mutate resources other
  people rely on.
- Use clearly-labeled, unique keys for anything you create (e.g.
  `cli-validation-<short-desc>-<timestamp>`).
- Clean up what you create. Every `create` in your validation should have a
  matching `delete` once you've captured the evidence.
- Never run destructive commands against production, and never point `base-uri`
  at production for validation.

## 5. Record a human-style video for the PR

Every PR that changes CLI behavior must include a screen recording of you using
the CLI the way a human would — typing the commands in a terminal and showing the
real output. This is what gives the reviewer confidence.

The recording must:

1. Show the build step (`make build`) and the version check, so it's clear the
   video uses the freshly built binary.
2. Type the actual commands that exercise the change (from step 3) in a visible
   terminal, one at a time, and show their real output — not a montage of
   pre-baked screenshots.
3. Demonstrate the meaningful result of the change (the created/updated resource,
   the new output format, the corrected error message, etc.).
4. **Never reveal secrets.** Pass the token via `LD_ACCESS_TOKEN` exported
   earlier (off-camera) so the token value never appears on screen. Do not `echo`
   the token or run `config --list` while a token is stored in the config file.

How to capture it in a Cloud Agent:

- Start a screen recording with the `RecordScreen` tool (`START_RECORDING`).
- Use the `computerUse` subagent to open a terminal and type/run the commands so
  the interaction looks like a real person using the CLI.
- Stop and save with `RecordScreen` (`SAVE_RECORDING`), giving it a descriptive
  name.
- Follow the `walkthrough-artifacts` skill for saving under `/opt/cursor/artifacts`
  and embedding the video in the PR body and your final summary with an HTML
  `<video>` tag pointing at the absolute file path.

If a staging token is unavailable, still record the local, no-auth portions you
can (build, help, config, output formatting, error paths) and state in the PR
that authenticated staging validation is pending the `LD_ACCESS_TOKEN` secret.

## 6. Before opening the PR — checklist

- [ ] `make build` succeeds and you tested `./ldcli` (the built binary).
- [ ] You ran the CLI against **staging** (not production) with the change's
      relevant commands, or documented the missing-secret blocker.
- [ ] Validation matched the change type (see step 3), including output formats
      and error paths where relevant.
- [ ] Any staging resources you created were cleaned up.
- [ ] Automated tests pass (`make test`; UI tests if you touched the dev-server UI).
- [ ] A human-style screen recording is attached to the PR, and it reveals no
      secrets.
