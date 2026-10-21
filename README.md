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

The AWS DevOps Agent is available in `us-east-1`, `us-east-2`, `us-west-2`, `ca-central-1`, `sa-east-1`, `ap-south-1`, `ap-southeast-1`, `ap-southeast-2`, `ap-northeast-1`, `ap-northeast-2`, `eu-central-1`, `eu-west-1`, `eu-west-2`, `eu-west-3` and `eu-north-1`, and requires AWS CLI 2.36 or later if you also use the AWS CLI directly. `setup` warns when the `aws` binary on your PATH is missing or older than that; the command itself uses the AWS SDK, so it still runs.

If the account already runs the AWS DevOps Agent, `setup` lists its agent spaces and asks which one to connect LaunchDarkly to. If you pick a space that `setup` did not make, `setup` only registers the LaunchDarkly MCP server and associates it with that space. It does not change the IAM roles, the account association, or the operator app sign-in configuration of that space. If the operator app of that space is off, `setup` tells you at the end and does not print the operator app URL. To turn the app on, use the AWS console. In the JSON result, `operatorAppEnabled` is then `false`. If you pick a space that `setup` made, `setup` runs the full setup on it. The full setup also changes the IAM roles, the account association, and the operator app. `setup` tags each space that it creates with `ldcli:managed=aws-devops-agent`. A space counts as made by `setup` only if it has that tag. A user can change the description, so `setup` does not read it. The list marks those spaces `made by ldcli`. If `setup` cannot read the tags of a space, the owner of that space is unknown. The list then leaves out that space, and a note names it. To use that space, pass `--agent-space-id <id>`. Its owner is still unknown, so `setup` then only connects the MCP server, unless you pass `--auth-flow`. If `setup` cannot read the tags of any space, it stops. If you pass `--agent-space-name` and a space has that name, you can press Enter to pick that space. If no space has that name, the list adds the choice to provision a new space with that name, and Enter picks it. Without `--agent-space-name`, Enter picks the one space that `setup` made. If there is no such space, or more than one, the list has no default, and you must type a number. If you choose to provision a new space without a name, `setup` asks for one. If a space in the region already uses the name, `setup` asks again.

To pick a space without the list, pass `--agent-space-id <id>`. To provision another space, pass `--new-agent-space`. You cannot pass both. If a space in the region already has the `--agent-space-name` name, `setup` does not provision a new space. Pass another name. If you pass `--auth-flow`, `setup` runs the full setup on any space that you pick. On a space that `setup` did not make, it prints a warning before the plan, and the plan names the current sign-in next to the new one.

Before `setup` changes AWS, it shows a plan. The plan names the AWS account, region, profile, IAM roles, agent space, operator app, and MCP server, and what `setup` will do to each. In a terminal, `setup` then asks `Continue? [y/N]` and stops with an error unless you type `y` or `yes`. To skip every question, pass `--yes`. Then `setup` does not ask, and it accepts each step. If stdin and stderr are terminals, `setup` can ask, also with `--output json`. Without a terminal, it does not ask. `setup` writes the plan, the questions, and the progress to stderr. It writes only the result to stdout. Thus every run shows the plan, and with `--output json`, stdout contains only the JSON result.

If the SSO session of your profile expired, `setup` and `status` offer to run `aws sso login --profile <name>` for you. They ask only if stdin and stderr are terminals. With `--yes`, `setup` runs `aws sso login` without a question. Without a terminal, they never run it, because the sign-in needs a person in a browser.

If `setup` cannot ask and you pass `--agent-space-name`, it does what Enter does at the list. If one space has that name, `setup` uses it, with the full setup only on a space that `setup` made. If no space has that name, `setup` provisions it. If two or more spaces have that name, `setup` stops and asks for `--agent-space-id`. Thus the same command works on the first run and on every later run in CI. Without `--agent-space-name`, `setup` reuses the one space that `setup` made. If there is no such space, or more than one, `setup` stops. If `setup` cannot read the tags of a space, it stops without `--agent-space-name`. It also stops if that space has the `--agent-space-name` name. If `setup` must provision a space and cannot ask, you must pass `--agent-space-name`.

Otherwise `setup` is safe to re-run: it reuses the IAM roles, the agent space, the account and MCP associations on it, and any LaunchDarkly MCP server already registered on the account. If the operator app of a reused space is on, `setup` keeps its sign-in. To change the sign-in, pass `--auth-flow`. The IAM roles are account-wide and trust the agent spaces in every region, so running `setup` in a second region leaves the first one working; a role named `DevOpsAgentRole-AgentSpace` or `DevOpsAgentRole-WebappAdmin` that `setup` did not create is reused untouched, and it has to trust `aidevops.amazonaws.com` itself.

The AWS identity that runs `setup` needs these permissions:

- `aidevops:ListAgentSpaces`, `aidevops:GetAgentSpace`, `aidevops:CreateAgentSpace`, and `aidevops:ListTagsForResource`. To tag a new space, AWS can also require `aidevops:TagResource`.
- `aidevops:ListAssociations`, `aidevops:AssociateService`, `aidevops:ListServices`, and `aidevops:RegisterService`.
- `aidevops:GetOperatorApp` and `aidevops:EnableOperatorApp`.
- `aidevops:DisassociateService` and `aidevops:DeregisterService`, but only with `--replace-mcp-token`.
- `iam:GetRole`, `iam:ListRoleTags`, `iam:CreateRole`, `iam:TagRole`, `iam:AttachRolePolicy`, `iam:PutRolePolicy`, and `iam:UpdateAssumeRolePolicy`. AWS can also require `iam:PassRole` on the two roles.

`status` needs only the read permissions: `iam:GetRole`, `aidevops:ListServices`, `aidevops:ListAgentSpaces`, `aidevops:GetAgentSpace`, `aidevops:ListAssociations`, and `aidevops:GetOperatorApp`.

`--access-token` is optional. The first available token — the flag, then `LD_ACCESS_TOKEN`, then the one already in your ldcli configuration — is registered with AWS as the bearer token the agent uses to call the LaunchDarkly MCP server, so it should be a [service token](https://launchdarkly.com/docs/home/account/api-create) whose permissions match what you want the agent to do. AWS keeps it until the MCP server is re-registered, so a session token written by `ldcli login` eventually expires and the agent then fails with `unauthorized` errors. Re-run with `--access-token <token> --replace-mcp-token` to re-register an MCP server with a different token; the token is checked against LaunchDarkly first, because AWS stores it write-only and the previous registration cannot be restored. The MCP server already registered on the account is the one serving `https://mcp.launchdarkly.com/mcp/launchdarkly`, whatever it is named: pass `--mcp-endpoint` to connect a LaunchDarkly MCP server you host yourself, or `--mcp-service-id` to point at a specific registration. That registration must serve the LaunchDarkly endpoint, or the endpoint that you pass with `--mcp-endpoint`. The agent may call `list-projects`, `list-flags` and `get-flag` without asking, and must ask for approval before calling `toggle-flag`.

With no token configured at all, `setup` prints the LaunchDarkly page where you create a service token, and asks for the token before it shows the plan. So you do not need one ready beforehand. `setup` checks the token with LaunchDarkly. If LaunchDarkly rejects it, `setup` stops before it changes AWS. If you press Enter without a token, the plan skips the MCP server. A run that only connects the MCP server then has nothing to do, so `setup` stops before it changes AWS. If no MCP server is registered and `setup` cannot ask for a token, it stops before it changes AWS. That happens without a terminal and with `--yes`. In those runs, pass `--access-token`.

`setup` ends by printing the operator app URL, `https://<agent-space-id>.aidevops.global.app.aws`, which is where you use the agent, together with the AWS console page that manages it.

`ldcli aws-devops-agent status` shows what exists in the account and region.

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
