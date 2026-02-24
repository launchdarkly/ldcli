# Installing LaunchDarkly CLI Skills for Claude Code

These skills give AI agents (Claude Code) the knowledge to manage LaunchDarkly resources using `ldcli`.

## Prerequisites

- [Claude Code](https://claude.ai/claude-code) installed
- [`ldcli`](https://github.com/launchdarkly/ldcli) installed and on your PATH
- Authenticated: run `ldcli login` or set `LD_ACCESS_TOKEN`

## Quick Install

```bash
git clone https://github.com/launchdarkly/ldcli.git
cd ldcli/skills
./install.sh
```

That's it. The installer:

1. **Symlinks skill files** into `~/.claude/commands/` as slash commands (`/ld-feature-flags`, `/ld-flag-targeting`, etc.)
2. **Adds a block to `~/.claude/CLAUDE.md`** so Claude passively knows about `ldcli` in every session
3. **Creates `~/.claude/launchdarkly-conventions.md`** — a template for your org-specific customizations

## What Gets Installed

### Slash Commands

| Command | Skill |
|---------|-------|
| `/ld-feature-flags` | Create, list, get, toggle, archive, delete flags |
| `/ld-flag-targeting` | Targeting rules, rollouts, individual targets, prerequisites |
| `/ld-projects-and-environments` | Manage projects and environments |
| `/ld-segments` | Create and manage audience segments |
| `/ld-members-and-teams` | Invite members, manage teams and roles |
| `/ld-dev-server` | Local development server and flag overrides |
| `/ld-audit-and-observability` | Audit logs, contexts, metrics, experiments |

### Passive Context (`~/.claude/CLAUDE.md`)

A block is appended to your global `CLAUDE.md` that tells Claude about `ldcli` availability. This means you can just say "create a feature flag called dark-mode" and Claude will know to use `ldcli` — without invoking a slash command first.

### Conventions File (`~/.claude/launchdarkly-conventions.md`)

A template where you define your organization's standards. Uncomment and edit sections for:

- Default project and environment keys
- Flag naming conventions
- Tagging standards
- Safety rules (e.g., "always confirm before toggling in production")
- Team-specific context

Claude reads this file automatically and applies your conventions when working with `ldcli`.

## How It Works

The install uses **symlinks**, so the slash commands always reflect the latest skill files. If you `git pull` to update the skills, your Claude sessions pick up the changes immediately.

```
~/.claude/
├── CLAUDE.md                          ← passive ldcli context appended here
├── launchdarkly-conventions.md        ← YOUR org customizations (copied, not linked)
└── commands/
    ├── ld-feature-flags.md            → symlink to skills/feature-flags.md
    ├── ld-flag-targeting.md           → symlink to skills/flag-targeting.md
    ├── ld-projects-and-environments.md → symlink to skills/projects-and-environments.md
    ├── ld-segments.md                 → symlink to skills/segments.md
    ├── ld-members-and-teams.md        → symlink to skills/members-and-teams.md
    ├── ld-dev-server.md               → symlink to skills/dev-server.md
    └── ld-audit-and-observability.md  → symlink to skills/audit-and-observability.md
```

## Customization

### The Layered Approach

Skills are organized in two layers:

1. **Standard skills** (symlinked) — describe *how* to use `ldcli`. You don't need to modify these.
2. **Conventions file** (your copy) — describes *your team's standards*. This is where you customize.

For example, if your team always uses project key `my-app` and environments `dev`/`staging`/`prod`, you'd edit `~/.claude/launchdarkly-conventions.md`:

```markdown
## Default Project and Environments
- Default project key: `my-app`
- Environments: `dev`, `staging`, `prod`
- When I say "production", use environment key: `prod`
```

Then when you tell Claude "toggle dark-mode on in production," it knows to run:
```bash
ldcli flags toggle-on --project my-app --flag dark-mode --environment prod
```

### Forking Skills for Deep Customization

If you need to modify the skill files themselves (not just conventions), **copy instead of symlink**:

```bash
# Remove the symlink
rm ~/.claude/commands/ld-feature-flags.md

# Copy the file (now you own it)
cp skills/feature-flags.md ~/.claude/commands/ld-feature-flags.md

# Edit as needed
```

Note: copied files won't auto-update on `git pull`.

## Updating

```bash
cd ldcli/skills
git pull
# Symlinked skills update automatically. Re-run install.sh only if new skills were added:
./install.sh
```

The installer is idempotent — it skips files that are already correctly linked and preserves your conventions file.

## Uninstalling

```bash
cd ldcli/skills
./install.sh --uninstall
```

This removes the slash command symlinks and the `CLAUDE.md` block. Your conventions file is preserved (delete `~/.claude/launchdarkly-conventions.md` manually if you want a full cleanup).

## Manual Install

If you prefer not to use the script:

1. Create `~/.claude/commands/` if it doesn't exist
2. Symlink (or copy) each skill `.md` file into `~/.claude/commands/` with an `ld-` prefix
3. Add the ldcli context block to `~/.claude/CLAUDE.md` (see install.sh for the exact text)
4. Copy `conventions.md.example` to `~/.claude/launchdarkly-conventions.md` and customize it
