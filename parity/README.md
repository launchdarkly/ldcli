# Parity harness

`make parity` runs the same argv against the Go `ldcli` oracle and, for cases that opt in, the Rust `ldcli` binary. A failure prints the argv and a unified diff of exit status, stdout, stderr, and declared files.

Until a case sets `rust = true`, the check compares a fresh Go run with the transcript committed under `parity/cases/`. That is how the help corpus stays pinned to Go while the Rust CLI is still incomplete: a command the Rust binary has not implemented yet is recorded from Go and left uncompared until the unit that implements it flips the case on.

## Run

```bash
make parity
```

Go 1.25 or newer has to be on `PATH`. The Rust toolchain comes from `rust-toolchain.toml`.

The harness gives every case its own config directory and state directory, pins width to 80 by running without a terminal (the Go help path falls back to 80), sets `NO_COLOR=1`, sets the Go version label to `test`, turns analytics off, and turns the update check off. It unsets `LD_ACCESS_TOKEN`.

The parent's `PATH` is not passed on either. A case's `PATH` holds one directory, created in its sandbox, containing a shim for each program a CLI runs to open a browser: `xdg-open`, `x-www-browser`, and `www-browser` on Linux, `open` on macOS. Each shim prints `parity browser shim: <url>` on stderr and exits 1, so no run can open a real browser, and both binaries take the same browser-failure path whatever the machine has installed. The CLI's stderr is where that line lands, because Go hands its own stdout and stderr to the opener, so the transcript also shows which URL each binary tried to open. Nothing else is on `PATH`, so a command that looks for a package manager finds none and cannot run one, unless the case seeds a stand-in (see `bin:` seeds below). A case that sets `PATH` in `[env]` gets it after the shim directory.

## Read a diff

A failing case looks like this:

```text
case ldcli__flags
argv: flags --help
stdout differs
--- expected stdout
+++ go stdout
@@ -12 +12 @@
-      --alpha
+      --beta
```

When the Rust binary is included, the labels are `go` and `rust`. JSON object keys are sorted before that compare. Array order is kept. Help text, flag names, error sentences, and exit codes are compared as text.

A failing coverage gate still prints every command's state, with the offenders marked `UNCOVERED` in place.

## Redaction

Two things are always redacted, because they change on every run and belong to the harness rather than to the CLI: the case's sandbox path becomes `[SANDBOX]`, and any other temp path becomes `[TEMP]`. Access tokens, device codes, user codes, and verification URIs are always replaced with `[ACCESS_TOKEN]`, `[DEVICE_CODE]`, `[USER_CODE]`, and `[VERIFICATION_URI]` before anything is written to disk.

Everything else a command prints is compared byte for byte unless the case asks otherwise. Help text quotes UUIDs, timestamps, and version numbers as documentation, and masking those would hide a real difference between the two binaries. A case that prints a genuinely varying value names the rule it needs:

```toml
redact = ["uuid", "timestamp"]
```

| Rule | Replaces |
|---|---|
| `uuid` | any UUID with `[UUID]` |
| `timestamp` | any ISO-8601 timestamp with `[TIMESTAMP]` |
| `version` | any `x.y.z` version with `[VERSION]` |
| `hostname` | this machine's name with `[HOSTNAME]` |
| `update-notice` | the update-check notice block with `[UPDATE_NOTICE]` |
| `signup-browser` | signup's browser-failure warning detail |

Ask for `version` only on a case whose output carries a build version that the two binaries cannot be made to share. Both binaries are built with the version label `test`, so an ordinary case does not need it, and a `--version` case that redacts the version asserts nothing. Ask for `hostname` only on a case that prints the machine name, such as login's device name: the rule is a plain string replacement, so on a machine whose name is an ordinary word it would rewrite that word anywhere it appeared. An unknown rule name fails the case.

## Add a case

1. Add `parity/cases/<tier>/<name>.toml`. Tiers are `help`, `offline`, `http`, and `fs`.
2. Set `argv`, `declare`, and any extra `env`.
3. Declare every file the command may write under the config, state, or work directory. Root commands write `config:ldcli/config.yml` on startup. An undeclared file fails the case.
4. Add a `redact` list only if the command prints a value that changes between runs. See Redaction above.
5. Run `make parity-capture` to fill stdout, stderr, status, and declared files from the Go binary. Capture refuses a `--rust-bin`, so a Rust bug cannot become the expected output.
6. Run `make parity`. Commit the case only when the diff is a behavior change you mean to accept.

`declare` paths use `config:`, `state:`, and `work:` prefixes, relative to `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, and the directory the command runs in. The work directory starts empty; a command that writes into the project it was pointed at, such as `setup init`, declares the files it writes there.

A `[seed]` table writes files into the sandbox before the run, keyed the same way. A case that starts from an existing config file seeds it and declares it, and the harness then compares the bytes each binary leaves behind:

```toml
declare = ["config:ldcli/config.yml"]

[seed]
"config:ldcli/config.yml" = "output: markdown\nproject: proj\n"
```

A seed key that would leave the sandbox is refused. A seeded file is part of the snapshot like any other, so a `work:` seed is declared too, and the harness checks that neither binary changed it.

A `bin:<name>` seed writes an executable into the `PATH` directory beside the browser shims, so a case can show a command that finds a tool. The script runs under `/bin/sh` with nothing else on `PATH`, so it can use shell builtins only. Use it for a stand-in that prints and exits, never for one that reaches the real tool. A `bin:` seed cannot replace a browser shim, and it is not part of the snapshot:

```toml
[seed]
"work:package.json" = "{\"name\": \"app\"}\n"
"bin:npm" = "#!/bin/sh\necho \"added 1 package\"\n"
```

## HTTP fixtures

A command that talks to LaunchDarkly is pointed at a local fixture server instead. Each binary gets its own server, answering the case's `[[http]]` routes; a request with no matching route gets a 404 with an empty body.

```toml
argv = ["whoami", "--base-uri", "{{BASE_URI}}"]

[env]
LD_ACCESS_TOKEN = "{{ACCESS_TOKEN}}"

[[http]]
method = "GET"
path = "/api/v2/caller-identity"
status = 200
body = "{\"accountId\": \"acct-1\"}"
```

`{{BASE_URI}}` becomes that run's server address anywhere in `argv`, `env`, `seed`, or a route body. The harness also mints four values for each run and fills them in the same places:

| Placeholder | Minted value | Printed as |
|---|---|---|
| `{{ACCESS_TOKEN}}` | `parity-token-…` | `[ACCESS_TOKEN]` |
| `{{DEVICE_CODE}}` | `parity-device-…` | `[DEVICE_CODE]` |
| `{{USER_CODE}}` | `parity-user-…` | `[USER_CODE]` |
| `{{VERIFICATION_URI}}` | `/confirm-auth/parity-verify-…` | `[VERIFICATION_URI]` |

The server's address changes every run, so it reads `[BASE_URI]` wherever it is printed. Capture refuses to write a case whose output still holds a minted value after substitution, and check refuses any file under `parity/` that holds one or starts with one of those prefixes.

A route answers every request the same way unless it lists what comes next. Each `[[http.then]]` table after a route answers the next request, in order, and the last one repeats. This is how a polling client sees a pending answer before the final one:

```toml
[[http]]
method = "POST"
path = "/internal/device-authorization/token"
status = 400
body = "{\"code\": \"authorization_pending\"}"

[[http.then]]
status = 200
body = "{\"accessToken\": \"{{ACCESS_TOKEN}}\"}"
```

Every request the server receives is recorded, with its method, path and query, body, and the `Authorization`, `Content-Type`, `LD-API-Version`, and `User-Agent` headers, into a `.requests` file beside the transcript. A case compares those as well, so a binary that prints the right thing after sending the wrong request still fails.

Set `rust = true` once the Rust binary is supposed to match that case.

## Add an exemption

`parity/exemptions.toml` lists commands whose live process is not diffed. Each entry needs a `reason` and a non-empty `substitute`.

- `substitute = "help"` means the help transcript is the assertion.
- `substitute = "exit-0"` runs the command and checks that it exits 0. Script bytes are not compared. This is what `completion bash`, `completion zsh`, `completion fish`, and `completion powershell` use.
- `rust = true` runs the substitute against the Rust binary as well. Like a case's `rust` field it stays off until that command exists, so an unimplemented command is skipped rather than failing.
- `kind = "behavior"` is for a non-command carve-out such as analytics posts. The harness still refuses to write analytics bodies or `Authorization` values.

The coverage gate reads the Go command tree, including hidden commands, from `go run ./parity/dump`. A command with neither a help transcript nor an exemption fails `make parity`.

## Formatter fixtures

Transcripts cover a whole command. The output formatters are also compared on their own, because a canonical JSON diff can hide an indentation or column bug and because most renderings have no command wired up yet.

`make output-fixtures` runs a fixed set of payloads through Go's own `internal/output` and records what it produced under `parity/fixtures/output`. `make rust-test` compares the Rust formatters against those recordings. CI regenerates the fixtures and fails if that leaves a diff, so a change to Go output has to be recaptured in the same commit.

```text
cases.json     every payload rendered as json, plaintext, and markdown
errors.json    every error shape rendered in each kind
kinds.json     what NewOutputKind accepts and what it says when it does not
numbers.json   how a decoded value is written back out as JSON
sprint.json    how a decoded number is printed in plaintext
```

The last two exist because Go prints a number two different ways. Written back out as JSON, 1418684722483 stays itself. Printed in plaintext it goes through `fmt.Sprint` of a float64 and reads `1.418684722483e+12`. A decoded number is always a float64, so a value past 2^53 loses precision in both.

## Layout

```text
parity/cases/help/       one --help transcript per Go command
parity/cases/offline/    commands that do not need the network
parity/cases/http/       commands pointed at a fixture server
parity/cases/fs/         commands whose filesystem result is the assertion
parity/fixtures/output/  recorded renderings from the Go formatters
parity/exemptions.toml
parity/dump/             prints the Go command tree, including hidden commands
parity/output_fixtures/  records the formatter fixtures from the Go tree
```

Stdout and stderr are sibling files (`.stdout`, `.stderr`) so the transcripts stay readable. The TOML file holds argv, status, and declared file bytes.
