# Installing LaunchDarkly CLI Skills for Claude Code

These skills give AI agents (Claude Code) the knowledge to manage LaunchDarkly resources using `ldcli`.

## Prerequisites

- [Claude Code](https://claude.ai/claude-code) installed

`ldcli` does **not** need to be installed before running the skill installer. If it's missing, the installer will warn you and continue. Once the skills are installed, Claude will automatically detect that `ldcli` is missing when you ask it to do something with LaunchDarkly and offer to install it for you.

If you'd rather install `ldcli` yourself first:

```bash
# macOS
brew tap launchdarkly/homebrew-tap && brew install ldcli

# npm
npm install -g @launchdarkly/ldcli

# Then authenticate
ldcli login
# or set LD_ACCESS_TOKEN in your environment
```

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
| `/ld-setup` | Install, authenticate, and verify `ldcli` |
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
    ├── ld-setup.md                    → symlink to skills/setup.md
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

No script required. Just copy files and paste a text block.

### 1. Copy skill files

Create the commands directory if it doesn't exist, then copy (or symlink) each skill file with an `ld-` prefix:

```bash
mkdir -p ~/.claude/commands

# From the skills/ directory, copy each file:
cp setup.md              ~/.claude/commands/ld-setup.md
cp feature-flags.md      ~/.claude/commands/ld-feature-flags.md
cp flag-targeting.md     ~/.claude/commands/ld-flag-targeting.md
cp projects-and-environments.md ~/.claude/commands/ld-projects-and-environments.md
cp segments.md           ~/.claude/commands/ld-segments.md
cp members-and-teams.md  ~/.claude/commands/ld-members-and-teams.md
cp dev-server.md         ~/.claude/commands/ld-dev-server.md
cp audit-and-observability.md ~/.claude/commands/ld-audit-and-observability.md
```

Or use symlinks if you want auto-updates on `git pull` (replace `cp` with `ln -s` using absolute paths).

### 2. Add context to CLAUDE.md

Open (or create) `~/.claude/CLAUDE.md` and paste this block:

```markdown
<!-- ldcli-skills -->
## LaunchDarkly CLI (ldcli)

When the user asks about feature flags, environments, projects, segments, or other LaunchDarkly resources, use `ldcli` to fulfill the request. Always use `-o json` for parseable output.

**Before your first ldcli command in a session**, verify it is available by running `which ldcli`. If ldcli is not found, tell the user and use `/ld-setup` for install and auth instructions.

**Skill commands available:** Use `/ld-setup`, `/ld-feature-flags`, `/ld-flag-targeting`, `/ld-projects-and-environments`, `/ld-segments`, `/ld-members-and-teams`, `/ld-dev-server`, or `/ld-audit-and-observability` to load detailed usage reference for a specific area.

**Before making changes:** Always list/get resources first to confirm keys exist. Use `ldcli <resource> --help` to check available flags for any command.

**Conventions:** See ~/.claude/launchdarkly-conventions.md for organization-specific naming, tagging, and safety conventions.
<!-- ldcli-skills -->
```

### 3. Set up your conventions file (optional)

```bash
cp conventions.md.example ~/.claude/launchdarkly-conventions.md
```

Edit `~/.claude/launchdarkly-conventions.md` to add your org's project keys, environment names, naming conventions, and safety rules.

### Manual uninstall

```bash
# Remove slash commands
rm ~/.claude/commands/ld-setup.md
rm ~/.claude/commands/ld-feature-flags.md
rm ~/.claude/commands/ld-flag-targeting.md
rm ~/.claude/commands/ld-projects-and-environments.md
rm ~/.claude/commands/ld-segments.md
rm ~/.claude/commands/ld-members-and-teams.md
rm ~/.claude/commands/ld-dev-server.md
rm ~/.claude/commands/ld-audit-and-observability.md

# Remove the ldcli block from ~/.claude/CLAUDE.md
# Delete everything between the two <!-- ldcli-skills --> markers (inclusive)

# Optionally remove conventions
rm ~/.claude/launchdarkly-conventions.md
```
