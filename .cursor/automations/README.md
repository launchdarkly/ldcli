# Agent automations

Prompts for maintainer-run agents. Contributors do not need them to work on ldcli.

## Dependabot upgrade verification

- **Prompt:** [`dependabot-upgrade-verification.md`](dependabot-upgrade-verification.md)
- **Lookup table:** [`ldcli-surfaces.md`](ldcli-surfaces.md)

The agent produces a report on whether a Dependabot PR was exercised beyond CI. It never approves, merges, or pushes. It runs `ldcli` with analytics opted out and with temporary state and config directories, so it is safe to run on a workstation.

Suggested trigger: a Dependabot PR opened or updated, or a manual request with a PR URL. Posting the report as a PR comment is optional. The repository is public, so anything posted is public.
