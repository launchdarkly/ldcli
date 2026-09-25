# Parity harness

`make parity` runs the same argv against the Go `ldcli` oracle and, for cases that opt in, the Rust `ldcli` binary. A failure prints the argv and a unified diff of exit status, stdout, stderr, and declared files.

Until a case sets `rust = true`, the check compares a fresh Go run with the transcript committed under `parity/cases/`. That is how the help corpus stays pinned to Go while the Rust CLI is still incomplete: a command the Rust binary has not implemented yet is recorded from Go and left uncompared until the unit that implements it flips the case on.

## Run

```bash
make parity
```

Go 1.25 or newer has to be on `PATH`. The Rust toolchain comes from `rust-toolchain.toml`.

The harness gives every case its own config directory and state directory, pins width to 80 by running without a terminal (the Go help path falls back to 80), sets `NO_COLOR=1`, sets the Go version label to `test`, turns analytics off, and turns the update check off. It unsets `LD_ACCESS_TOKEN`.

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
3. Declare every file the command may write under the config or state directory. Root commands write `config:ldcli/config.yml` on startup. An undeclared file fails the case.
4. Add a `redact` list only if the command prints a value that changes between runs. See Redaction above.
5. Run `make parity-capture` to fill stdout, stderr, status, and declared files from the Go binary. Capture refuses a `--rust-bin`, so a Rust bug cannot become the expected output.
6. Run `make parity`. Commit the case only when the diff is a behavior change you mean to accept.

`declare` paths use `config:` and `state:` prefixes, relative to `XDG_CONFIG_HOME` and `XDG_STATE_HOME`.

Set `rust = true` once the Rust binary is supposed to match that case.

## Add an exemption

`parity/exemptions.toml` lists commands whose live process is not diffed. Each entry needs a `reason` and a non-empty `substitute`.

- `substitute = "help"` means the help transcript is the assertion.
- `substitute = "exit-0"` runs the command and checks that it exits 0. Script bytes are not compared. This is what `completion bash`, `completion zsh`, `completion fish`, and `completion powershell` use.
- `rust = true` runs the substitute against the Rust binary as well. Like a case's `rust` field it stays off until that command exists, so an unimplemented command is skipped rather than failing.
- `kind = "behavior"` is for a non-command carve-out such as analytics posts. The harness still refuses to write analytics bodies or `Authorization` values.

The coverage gate reads the Go command tree, including hidden commands, from `go run ./parity/dump`. A command with neither a help transcript nor an exemption fails `make parity`.

## Layout

```text
parity/cases/help/       one --help transcript per Go command
parity/cases/offline/    commands that do not need the network
parity/cases/http/       commands pointed at a fixture server
parity/cases/fs/         commands whose filesystem result is the assertion
parity/fixtures/         placeholder fixtures only
parity/exemptions.toml
parity/dump/             prints the Go command tree, including hidden commands
```

Stdout and stderr are sibling files (`.stdout`, `.stderr`) so the transcripts stay readable. The TOML file holds argv, status, and declared file bytes.
