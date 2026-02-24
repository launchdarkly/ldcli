#!/usr/bin/env bash
set -euo pipefail

# LaunchDarkly CLI Skills Installer for Claude Code
# Installs ldcli agent skills as slash commands and passive context.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="${HOME}/.claude"
COMMANDS_DIR="${CLAUDE_DIR}/commands"
CLAUDE_MD="${CLAUDE_DIR}/CLAUDE.md"
CONVENTIONS_FILE="${CLAUDE_DIR}/launchdarkly-conventions.md"

# Skill files to install (everything except non-skill files)
SKILL_FILES=(
  "feature-flags.md"
  "flag-targeting.md"
  "projects-and-environments.md"
  "segments.md"
  "members-and-teams.md"
  "dev-server.md"
  "audit-and-observability.md"
)

# The block we add to CLAUDE.md (used for detection and content)
CLAUDE_MD_MARKER="<!-- ldcli-skills -->"

print_step() {
  echo "  → $1"
}

print_success() {
  echo "  OK: $1"
}

print_skip() {
  echo "  skip: $1"
}

# --- Uninstall mode ---
if [ "${1:-}" = "--uninstall" ]; then
  echo ""
  echo "Uninstalling LaunchDarkly CLI Skills"
  echo "====================================="
  echo ""

  print_step "Removing slash commands..."
  for file in "${SKILL_FILES[@]}"; do
    cmd_name="${file%.md}"
    dest="${COMMANDS_DIR}/ld-${cmd_name}.md"
    if [ -L "$dest" ] || [ -f "$dest" ]; then
      rm "$dest"
      print_success "Removed /ld-${cmd_name}"
    fi
  done

  print_step "Removing ldcli block from CLAUDE.md..."
  if [ -f "$CLAUDE_MD" ] && grep -q "$CLAUDE_MD_MARKER" "$CLAUDE_MD"; then
    tmp_file=$(mktemp)
    awk -v marker="$CLAUDE_MD_MARKER" '
      BEGIN { skip=0 }
      $0 == marker { skip=!skip; next }
      !skip { print }
    ' "$CLAUDE_MD" > "$tmp_file"
    # Remove trailing blank lines left behind
    sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' "$tmp_file" > "$CLAUDE_MD"
    rm "$tmp_file"
    print_success "Removed ldcli block from CLAUDE.md"
  else
    print_skip "No ldcli block found in CLAUDE.md"
  fi

  echo ""
  echo "Note: ${CONVENTIONS_FILE} was NOT removed (preserving your customizations)."
  echo "      Delete it manually if you no longer need it."
  echo ""
  exit 0
fi

echo ""
echo "LaunchDarkly CLI Skills for Claude Code"
echo "========================================"
echo ""

# --- Step 1: Create commands directory ---
print_step "Checking ${COMMANDS_DIR}..."
if [ ! -d "$COMMANDS_DIR" ]; then
  mkdir -p "$COMMANDS_DIR"
  print_success "Created ${COMMANDS_DIR}"
else
  print_skip "Already exists"
fi

# --- Step 2: Symlink skill files ---
echo ""
print_step "Installing skill commands..."
for file in "${SKILL_FILES[@]}"; do
  src="${SCRIPT_DIR}/${file}"
  # Strip .md extension for the command name
  cmd_name="${file%.md}"
  dest="${COMMANDS_DIR}/ld-${cmd_name}.md"

  if [ ! -f "$src" ]; then
    echo "  WARN: ${file} not found in ${SCRIPT_DIR}, skipping"
    continue
  fi

  if [ -L "$dest" ]; then
    # Already a symlink - check if it points to the right place
    current_target="$(readlink "$dest")"
    if [ "$current_target" = "$src" ]; then
      print_skip "/ld-${cmd_name} already linked"
      continue
    fi
    rm "$dest"
  elif [ -f "$dest" ]; then
    echo "  WARN: ${dest} exists as a regular file, skipping (remove it manually to install)"
    continue
  fi

  ln -s "$src" "$dest"
  print_success "/ld-${cmd_name} → ${file}"
done

# --- Step 3: Add CLAUDE.md block ---
echo ""
print_step "Configuring ${CLAUDE_MD}..."

CLAUDE_MD_BLOCK="${CLAUDE_MD_MARKER}
## LaunchDarkly CLI (ldcli)

You have access to \`ldcli\`, the LaunchDarkly CLI, for managing feature flags, projects, environments, segments, and more. Always use \`-o json\` for parseable output.

**Skill commands available:** Use \`/ld-feature-flags\`, \`/ld-flag-targeting\`, \`/ld-projects-and-environments\`, \`/ld-segments\`, \`/ld-members-and-teams\`, \`/ld-dev-server\`, or \`/ld-audit-and-observability\` to load detailed usage reference for a specific area.

**Before making changes:** Always list/get resources first to confirm keys exist. Use \`ldcli <resource> --help\` to check available flags for any command.

**Conventions:** See ${CONVENTIONS_FILE} for organization-specific naming, tagging, and safety conventions.
${CLAUDE_MD_MARKER}"

if [ -f "$CLAUDE_MD" ] && grep -q "$CLAUDE_MD_MARKER" "$CLAUDE_MD"; then
  # Replace existing block: remove from first marker to second marker, write new block
  tmp_file=$(mktemp)
  awk -v marker="$CLAUDE_MD_MARKER" '
    BEGIN { skip=0 }
    $0 == marker { skip=!skip; next }
    !skip { print }
  ' "$CLAUDE_MD" > "$tmp_file"
  mv "$tmp_file" "$CLAUDE_MD"
  # Now append the fresh block
  echo "" >> "$CLAUDE_MD"
  echo "$CLAUDE_MD_BLOCK" >> "$CLAUDE_MD"
  print_success "Updated existing ldcli block in CLAUDE.md"
else
  # Append new block
  if [ -f "$CLAUDE_MD" ]; then
    echo "" >> "$CLAUDE_MD"
  fi
  echo "$CLAUDE_MD_BLOCK" >> "$CLAUDE_MD"
  print_success "Added ldcli block to CLAUDE.md"
fi

# --- Step 4: Install conventions template ---
echo ""
print_step "Checking conventions file..."
if [ ! -f "$CONVENTIONS_FILE" ]; then
  cp "${SCRIPT_DIR}/conventions.md.example" "$CONVENTIONS_FILE"
  print_success "Created ${CONVENTIONS_FILE}"
  echo "         Edit this file to add your org's naming, tagging, and safety conventions."
else
  print_skip "Already exists (your customizations are preserved)"
fi

# --- Done ---
echo ""
echo "Installation complete!"
echo ""
echo "What was installed:"
echo "  1. Slash commands: /ld-feature-flags, /ld-flag-targeting, etc."
echo "  2. Passive context in ~/.claude/CLAUDE.md (agent auto-discovers ldcli)"
echo "  3. Conventions file at ${CONVENTIONS_FILE}"
echo ""
echo "Next steps:"
echo "  - Edit ${CONVENTIONS_FILE} to match your org's conventions"
echo "  - Start a Claude Code session and try: \"list all flags in my project\""
echo "  - Or use a slash command: /ld-feature-flags"
echo ""
echo "To uninstall, run: $0 --uninstall"
echo ""
