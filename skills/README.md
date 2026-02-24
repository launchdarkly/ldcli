# LaunchDarkly CLI Agent Skills

AI agent skills for managing LaunchDarkly via `ldcli` — an alternative to the LaunchDarkly MCP server that works with any agent that can run shell commands.

## Quick Start

```bash
# Install skills into Claude Code
cd skills/
./install.sh
```

See [INSTALL.md](./INSTALL.md) for full setup instructions, customization, and uninstall.

## Prerequisites

- `ldcli` installed and on PATH
- Authenticated via `ldcli login` or `--access-token` / `LD_ACCESS_TOKEN` env var
- For JSON output (recommended for agents), use `-o json` on any command

## Skills

| Skill | Description |
|-------|-------------|
| [feature-flags](./feature-flags.md) | Create, read, update, toggle, and delete feature flags |
| [flag-targeting](./flag-targeting.md) | Manage flag targeting rules, rollouts, and individual targets |
| [projects-and-environments](./projects-and-environments.md) | Manage projects and environments |
| [segments](./segments.md) | Create and manage audience segments |
| [members-and-teams](./members-and-teams.md) | Invite members, manage teams and roles |
| [dev-server](./dev-server.md) | Run a local dev server for flag overrides |
| [audit-and-observability](./audit-and-observability.md) | Query audit logs, contexts, and metrics |

## Global Flags

All commands support these flags:

| Flag | Description |
|------|-------------|
| `--access-token <token>` | LaunchDarkly API access token (or set `LD_ACCESS_TOKEN`) |
| `-o json` | Output as JSON (recommended for programmatic use) |
| `--base-uri <uri>` | Custom LaunchDarkly instance URI |
| `-h` / `--help` | Help for any command |

## Tips for Agents

1. **Always use `-o json`** for parseable output. Default `plaintext` is human-readable but harder to parse.
2. **Discover available resources** with `ldcli resources` to see all resource commands.
3. **Get help** on any command: `ldcli <command> [subcommand] --help`
4. **List before acting**: always list/get resources first to confirm keys and IDs before making changes.
5. **Semantic patch** for flag updates: use `--semantic-patch` with `ldcli flags update` for readable, intent-based changes.

## Customization

After installing, edit `~/.claude/launchdarkly-conventions.md` to set your org's defaults (project key, environments, naming conventions, safety rules). The skills describe *how* to use ldcli; the conventions file describes *your* standards. See [INSTALL.md](./INSTALL.md#customization) for details.
