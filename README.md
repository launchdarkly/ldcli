[![NPM][npm-badge]][npm-link]
[![Docker][docker-badge]][docker-link]
[![GitHub release][ghrelease-badge]][ghrelease-link]

# LaunchDarkly CLI

The LaunchDarkly CLI helps you manage your feature flags from your terminal or your IDE.

With the CLI, you can:

- Create and evaluate your first feature flag with a guided `setup` command.
- Onboard your whole team by inviting new members.
- Interact with the [LaunchDarkly API](https://apidocs.launchdarkly.com/) using resource- and CRUD-based commands.

## Installation

The LaunchDarkly CLI is available for macOS, Windows, and Linux.

### macOS
The CLI is available on macOS via [Homebrew](https://brew.sh/):
```shell
brew tap launchdarkly/homebrew-tap
brew install ldcli
```

### Windows
A Windows executable of `ldcli` is available on the [releases page](https://github.com/launchdarkly/ldcli/releases).

### Linux
A Linux executable of `ldcli` is available on the [releases page](https://github.com/launchdarkly/ldcli/releases).

### Additional installations

You can also install the LaunchDarkly CLI using npm or Docker.

#### npm
Install with npm:
```shell
npm install -g @launchdarkly/ldcli
```

#### Docker
Pull from Docker:
```shell
docker pull launchdarkly/ldcli
```

## Usage

Installing the CLI provides access to the `ldcli` command.

```sh-session
ldcli [command]

# Run `--help` for detailed information about CLI commands
ldcli --help
```

## Configuration

The LaunchDarkly CLI allows you to save preferred settings, either as environment variables or within a config file. Use the `config` commands to save your settings.

Supported settings:

* `access-token` A LaunchDarkly access token with write-level access
* `analytics-opt-out` Opt out of analytics tracking (default false)
* `base-uri` LaunchDarkly base URI (default "https://app.launchdarkly.com")
- `environment`: Default environment key
- `flag`: Default feature flag key
- `output`: Output format: json or plaintext (default: plaintext in a terminal, json otherwise)
- `project`: Default project key

Available `config` commands:

- `config --set {key} {value}`
- `config --unset {key}`
- `config --list`

To save a setting as an environment variable, prepend the variable name with `LD`. For example:

```shell
export LD_ACCESS_TOKEN=api-00000000-0000-0000-0000-000000000000
```

To save a setting in the configuration file:

```shell
ldcli config --set access-token api-00000000-0000-0000-0000-000000000000
```

Running this command creates a configuration file located at `$XDG_CONFIG_HOME/ldcli/config.yml` with the access token. Subsequent commands read from this file, so you do not need to specify the access token each time.

### Output format defaults

When you do not pass `--output` or `--json`, the default format depends on whether standard output is a terminal: **plaintext** in an interactive terminal, **json** when stdout is not a TTY (for example when piped, in CI, or in agent environments).

To force the plaintext default even when stdout is not a TTY, set either **`FORCE_TTY`** or **`LD_FORCE_TTY`** to any non-empty value (similar to tools that use `NO_COLOR`). That only affects the default; explicit `--output`, `--json`, `LD_OUTPUT`, and the `output` setting in your config file still apply.

**`LD_OUTPUT`** is the same setting as `output` in the config file, exposed as an environment variable (see the `LD_` prefix above). It is not new with TTY detection; the test suite locks in that it overrides the non-TTY JSON default when set to `plaintext`.

Effective output is resolved in this order: **`--json`** (if set, wins over `--output` when both are present), then **`--output`**, then **`LD_OUTPUT`**, then the **`output`** value from your config file, then the TTY-based default above.

## Commands

LaunchDarkly CLI commands:

- `setup` guides you through creating your first flag, connecting an SDK, and evaluating your flag in your Test environment
- `dev-server` lets you start a local server and retrieve flag values from a LaunchDarkly source environment so you can test your code locally. For assistance starting with or running dev-server, refer to the [reference docs](https://launchdarkly.com/docs/guides/flags/ldcli-dev-server).
- `aws-devops-agent` provisions the AWS DevOps Agent integration in your own AWS account

### AWS DevOps Agent

`ldcli aws-devops-agent` creates the AWS resources the [AWS DevOps Agent](https://aws.amazon.com/devops-agent/) needs to manage your LaunchDarkly flags: the IAM roles it assumes, an agent space, the association with your AWS account, the operator web app, and the LaunchDarkly MCP server connection.

The commands use your existing AWS session rather than any cross-account LaunchDarkly role, so authenticate first. They fail before creating anything if no credentials are available:

```sh-session
aws sso login --profile my-profile
export AWS_PROFILE=my-profile AWS_REGION=us-east-1

ldcli aws-devops-agent setup
```

The AWS DevOps Agent is available in `us-east-1`, `us-west-2`, `ap-southeast-2`, `ap-northeast-1`, `eu-central-1` and `eu-west-1`, and requires AWS CLI 2.36 or later if you also use the AWS CLI directly. `setup` warns when the `aws` binary on your PATH is missing or older than that; the command itself uses the AWS SDK, so it still runs.

`setup` is safe to re-run: it reuses the IAM roles, the agent space matching `--agent-space-name`, the account and MCP associations on it, and any LaunchDarkly MCP server already registered on the account. Pass `--new-agent-space` to create an additional agent space instead.

`--access-token` is optional. When you pass one it is registered with AWS as the bearer token the agent uses to call the LaunchDarkly MCP server, so it should be a token whose permissions match what you want the agent to do. By default the agent may call `list-projects`, `list-flags` and `get-flag` without asking, and must ask for approval before calling `toggle-flag`. Use `--mcp-read-only-tools` and `--mcp-mutative-tools` to change that. Without a token, `setup` finishes the AWS side and prints the console URL where you can add the MCP server yourself; `--skip-mcp-server` leaves it out entirely.

Two steps cannot be automated because they are browser consent flows: registering and installing the GitHub App, and creating a Kiro API key. In a terminal, `setup` pauses on each one, showing a single URL to open. It resumes when you press Enter, except for GitHub, where it polls AWS and continues on its own once the registration appears — and connects the repository immediately if you passed `--github-owner`, `--github-repo` and `--github-repo-id`. GitHub is skipped entirely when the account already has a registration. Press `s` to skip a step, or pass `--no-wait` to list them all instead, for example in CI:

```sh-session
ldcli aws-devops-agent setup --no-wait \
  --github-owner <owner> --github-repo <repo> --github-repo-id <repo-id>
```

`ldcli aws-devops-agent status` shows what exists. `ldcli aws-devops-agent teardown` removes everything in the account and region — every agent space, the registered services including GitHub and the MCP server, and the IAM roles — after asking you to confirm (`--force` skips the prompt, and is required when there is no terminal). Flags narrow it instead: `--agent-space-id <agent-space-id>`, `--service-id <service-id>`, `--deregister-github` and `--delete-roles`.

### Resource Commands

Resource commands mirror the LaunchDarkly API and make requests for a given resource. To see a full list of resources supported by the CLI, enter `ldcli --help` into your terminal.

To see the commands available for a given resource:

```sh-session
ldcli <resource> --help
```

Here is an example command to create a flag:

```sh-session
ldcli flags create --access-token <access-token> --project default --data '{"name": "My Test Flag", "key": "my-test-flag"}'
```

## Documentation

Additional documentation is available at https://docs.launchdarkly.com/home/getting-started/ldcli.

## Contributing

We encourage pull requests and other contributions from the community. Check out our [contributing guidelines](CONTRIBUTING.md) for instructions on how to contribute to this project.

### Running a local build of the CLI
If you wish to test your changes locally, simply
1. Clone this repo to your local machine;
2. Run `make build` from the repo root;
3. Run commands as usual with `./ldcli`.

## Verifying build provenance with the SLSA framework

LaunchDarkly uses the [SLSA framework](https://slsa.dev/spec/v1.0/about) (Supply-chain Levels for Software Artifacts) to help developers make their supply chain more secure by ensuring the authenticity and build integrity of our published packages. To learn more, see the [provenance guide](./PROVENANCE.md).

## About LaunchDarkly

* LaunchDarkly is a continuous delivery platform that provides feature flags as a service and allows developers to iterate quickly and safely. We allow you to easily flag your features and manage them from the LaunchDarkly dashboard.  With LaunchDarkly, you can:
    * Roll out a new feature to a subset of your users (like a group of users who opt-in to a beta tester group), gathering feedback and bug reports from real-world use cases.
    * Gradually roll out a feature to an increasing percentage of users, and track the effect that the feature has on key metrics (for instance, how likely is a user to complete a purchase if they have feature A versus feature B?).
    * Turn off a feature that you realize is causing performance problems in production, without needing to re-deploy, or even restart the application with a changed configuration file.
    * Grant access to certain features based on user attributes, like payment plan (eg: users on the ‘gold’ plan get access to more features than users in the ‘silver’ plan). Disable parts of your application to facilitate maintenance, without taking everything offline.
* LaunchDarkly provides feature flag SDKs for a wide variety of languages and technologies. Read [our documentation](https://docs.launchdarkly.com/sdk) for a complete list.
* Explore LaunchDarkly
    * [launchdarkly.com](https://www.launchdarkly.com/ "LaunchDarkly Main Website") for more information
    * [docs.launchdarkly.com](https://docs.launchdarkly.com/  "LaunchDarkly Documentation") for our documentation and SDK reference guides
    * [apidocs.launchdarkly.com](https://apidocs.launchdarkly.com/  "LaunchDarkly API Documentation") for our API documentation
    * [blog.launchdarkly.com](https://blog.launchdarkly.com/  "LaunchDarkly Blog Documentation") for the latest product updates

[npm-badge]: https://img.shields.io/npm/v/@launchdarkly/ldcli.svg?style=flat-square
[npm-link]: https://www.npmjs.com/package/@launchdarkly/ldcli

[docker-badge]: https://img.shields.io/docker/v/launchdarkly/ldcli.svg?style=flat-square&label=Docker
[docker-link]: https://hub.docker.com/r/launchdarkly/ldcli

[ghrelease-badge]: https://img.shields.io/github/release/launchdarkly/ldcli.svg
[ghrelease-link]: https://github.com/launchdarkly/ldcli/releases/latest
