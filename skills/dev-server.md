# Skill: Dev Server

Run a local development server that mirrors LaunchDarkly flag state and allows local overrides. This is useful for local development and testing without affecting real environments.

## Starting the Dev Server

```bash
# Start with a specific project and source environment
ldcli dev-server start --project <project-key> --source <env-key>

# Start with a custom context
ldcli dev-server start --project <project-key> --source <env-key> \
  --context '{"kind": "user", "key": "test-user", "email": "test@example.com"}'

# Start with initial flag overrides
ldcli dev-server start --project <project-key> --source <env-key> \
  --override '{"my-flag": true, "banner-color": "red"}'

# Start on a custom port
ldcli dev-server start --port 9000

# Start without continuous sync (snapshot mode)
ldcli dev-server start --project <project-key> --source <env-key> --sync-once
```

The dev server runs on `http://localhost:8765` by default.

## Open the UI

```bash
ldcli dev-server ui
```

Opens the dev server's web UI in your default browser for visual flag management.

## Managing Projects

The dev server can track multiple projects simultaneously.

```bash
# List all projects configured in the dev server
ldcli dev-server list-projects -o json

# Add a project (copies flags from the source environment)
ldcli dev-server add-project --project <project-key> --source <env-key>

# Add a project with a custom context
ldcli dev-server add-project --project <project-key> --source <env-key> \
  --context '{"kind": "user", "key": "dev-user"}'

# Get project details
ldcli dev-server get-project --project <project-key> -o json

# Get project with overrides and available variations
ldcli dev-server get-project --project <project-key> --expand overrides --expand availableVariations -o json

# Sync a project's flags from LaunchDarkly
ldcli dev-server sync-project --project <project-key>

# Update a project's source or context
ldcli dev-server update-project --project <project-key> --source <new-env-key>
ldcli dev-server update-project --project <project-key> \
  --context '{"kind": "user", "key": "different-user"}'

# Remove a project
ldcli dev-server remove-project --project <project-key>
```

## Managing Flag Overrides

Override flag values locally without affecting the real LaunchDarkly environment.

```bash
# Override a boolean flag
ldcli dev-server add-override --project <project-key> --flag <flag-key> --data 'true'

# Override a string flag
ldcli dev-server add-override --project <project-key> --flag <flag-key> --data '"new-value"'

# Override a JSON flag
ldcli dev-server add-override --project <project-key> --flag <flag-key> \
  --data '{"enabled": true, "limit": 500}'

# Override a number flag
ldcli dev-server add-override --project <project-key> --flag <flag-key> --data '42'

# Remove a single override (reverts to synced value)
ldcli dev-server remove-override --project <project-key> --flag <flag-key>

# Remove all overrides for a project
ldcli dev-server remove-overrides --project <project-key>
```

## Import/Export

```bash
# Import a project from a JSON file (server need not be running)
ldcli dev-server import-project --project <project-key> --file ./project-backup.json
```

## Common Workflows

### Set up local development

```bash
# 1. Start the dev server with your project
ldcli dev-server start --project my-app --source development

# 2. Point your SDK to the dev server instead of LaunchDarkly
#    SDK base URI: http://localhost:8765
#    (Configure this in your application's LaunchDarkly SDK initialization)

# 3. Override flags as needed for testing
ldcli dev-server add-override --project my-app --flag new-feature --data 'true'

# 4. When done, remove overrides
ldcli dev-server remove-overrides --project my-app
```

### Test different flag configurations

```bash
# Override multiple flags for a test scenario
ldcli dev-server add-override --project my-app --flag feature-a --data 'true'
ldcli dev-server add-override --project my-app --flag feature-b --data '"variant-2"'
ldcli dev-server add-override --project my-app --flag rate-limit --data '1000'

# Run your tests...

# Reset to production-like values
ldcli dev-server remove-overrides --project my-app
ldcli dev-server sync-project --project my-app
```

## Dev Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8765` | Port for the dev server |
| `--dev-stream-uri` | `https://stream.launchdarkly.com` | Streaming endpoint for flag data |
| `--cors-enabled` | `false` | Enable CORS headers |
| `--cors-origin` | `*` | Allowed CORS origin |

## Notes

- The dev server uses SQLite locally to store flag state and overrides.
- The `--data` value for overrides is the JSON representation of the variation value (not the variation index).
- The `--source` environment is where the dev server copies initial flag configurations from.
- The `--sync-once` flag fetches flag data once instead of streaming continuously.
- The dev server supports both server-side and client-side SDK protocols.
