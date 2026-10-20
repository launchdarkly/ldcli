# What a check can use

A check is a bash script. It can use the tools below. Choose the tool that runs the code the way a user runs it.

## The three versions

| Side | Worktree | What it is |
|---|---|---|
| `base` | `work/base` | The last release tag, for example `v3.12.0` |
| `pr` | `work/pr` | The release candidate, the head of the release PR |
| `revert` | `work/revert-<hash>` | The candidate without the commits in the `revert` field of the check |

The runner sets `WT` to the worktree of the side under test. `BASE_WT` and `PR_WT` are also set, so a check on the candidate can build the last release, for example to make an old data file.

## The ldcli binary

`ldcli_bin` builds `$WT` once for each commit and prints the path of the binary. All checks share the build.

```bash
bin=$(ldcli_bin) || incomplete "build failed"
old=$(WT="$BASE_WT" ldcli_bin) || incomplete "could not build the last release"
```

Pass `--analytics-opt-out` or keep `LD_ANALYTICS_OPT_OUT=true`, which the runner sets.

## The dev server

Start it on a free port, with a placeholder token and a private state directory. With no project configured, it does not connect to LaunchDarkly.

```bash
port=$(free_port)
XDG_STATE_HOME="$ARTIFACTS/state" "$bin" dev-server start --port "$port" --access-token placeholder >"$ARTIFACTS/dev-server.log" 2>&1 &
trap 'kill $! 2>/dev/null' EXIT
wait_http "http://127.0.0.1:$port/dev/projects" || fail "the dev server did not start"
```

To test an upgrade, start the last release with the same `XDG_STATE_HOME`, and add data through its API. Then stop it, and start the candidate.

## A fake LaunchDarkly API

`fake_api_start <routes file>` starts `scripts/release/lib/fake_api.py`. It sets `FAKE_API_URI` and `FAKE_API_RECORD`. Pass `--base-uri "$FAKE_API_URI"` to ldcli. The record file has one JSON line for each request: method, path, query, and body. Use it to prove what ldcli sent, for example that a dry run sent no write, or that ldcli asked the user before a delete. The format of the routes file is in the comment at the top of `fake_api.py`.

Use the fake API for each command that writes to LaunchDarkly: `sync`, `flags` updates, `resources` writes, and setup commands.

## A live LaunchDarkly account (read only)

If the caller set `RELEASE_VERIFY_LIVE_TOKEN` and `RELEASE_VERIFY_LIVE_BASE_URI`, `verify.sh` starts `scripts/release/lib/readonly_proxy.py`. The proxy forwards only `GET` and `HEAD` requests, adds the token, and logs each request in `state/live-proxy.log`. It answers each write with 405.

```bash
live_api || incomplete "no live account for this run"
"$bin" projects list --base-uri "$LIVE_BASE_URI" --access-token placeholder -o json
```

Use the live account to prove that ldcli still works with the real API, for example after an update of the API client. `RELEASE_VERIFY_LIVE_PROJECT` names a project to read. The token never reaches the check.

A person can set `RELEASE_VERIFY_LIVE_WRITES=1` for a disposable sandbox account. Without it, never plan a check that writes.

## Fake external commands

Some commands run other programs, for example `aws-devops-agent setup` runs the AWS CLI. Put a fake script with the same name first on `PATH`. Make it record its arguments and print the answer that the real program gives.

```bash
mkdir -p "$ARTIFACTS/fakebin"
cat >"$ARTIFACTS/fakebin/aws" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_AWS_LOG"
case "$*" in
  "--version") echo "aws-cli/2.17.0 Python/3.12.6 Linux/6.0 exe/x86_64" ;;
  *) echo '{}' ;;
esac
EOF
chmod +x "$ARTIFACTS/fakebin/aws"
export FAKE_AWS_LOG="$ARTIFACTS/aws.log"
PATH="$ARTIFACTS/fakebin:$PATH" "$bin" aws-devops-agent setup ...
```

Never call a real cloud account from a check.

## Go tests

Copy a test file into the package under test, run only that test, and delete it. Name the file `zz_verify_*_test.go`. Keep the source in `generated/` with the extension `.txt`. A Go test can reach code that the CLI cannot reach easily, for example a limit of 10,000 rows. If the package or function does not exist on the last release, call `skip` there.

## Docker

If `docker info` works, a check can build and run images. The baseline already builds the release image (`docker-image`) and all release targets (`release-snapshot`).

## Other tools

`go`, `node` and `npm`, `python3`, `jq`, `curl`, `rg`, and `git` are available. `gh` is available with a read-only token. A browser is not available. For the UI, use the vitest tests or request the HTML and the API with `curl`.

## Limits

- The runner gives each check a private `HOME`, private XDG directories, and no `LD_*` variables. Do not read `~/.config/ldcli`.
- Avoid the network, except for the live proxy. If a check needs the network, say so in `rationale`, and report `incomplete` when the network fails.
- A check must not change files outside `$WT` and `$ARTIFACTS`.
