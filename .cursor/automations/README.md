# Agent automations

Prompts in this directory are meant to be pasted into a Cursor Automation or handed to a verification agent.

## Dependabot upgrade verification

**Prompt to paste:** [`dependabot-upgrade-verification.md`](dependabot-upgrade-verification.md)

**Repo lookup table:** [`ldcli-surfaces.md`](ldcli-surfaces.md) — the prompt tells the agent to read this when it is present.

Suggested automation setup:

- **Trigger:** Dependabot PR opened or updated on `launchdarkly/ldcli`, or a manual mention with a PR URL.
- **Goal:** Produce a dependency upgrade report. Include video only when a user-visible surface was actually exercised.
- **Do not:** auto-approve or auto-merge.

The four seed PRs used to shape the modes: #726 (`go-sqlite3`), #725 (`cobra`), #626 (`x/term`), #621 (`uber/mock`).
