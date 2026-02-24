# Skill: Feature Flags

Manage the lifecycle of feature flags: list, create, get, toggle, update, archive, and delete.

## List Flags

```bash
# List all flags in a project
ldcli flags list --project <project-key> -o json

# Filter by environment (reduces response size - recommended)
ldcli flags list --project <project-key> --env <env-key> -o json

# Search by name/key
ldcli flags list --project <project-key> --filter "query:my-flag" -o json

# Filter by tag
ldcli flags list --project <project-key> --tag "beta" -o json

# Filter by state (live, deprecated, archived)
ldcli flags list --project <project-key> --filter "state:live" -o json

# Paginate results
ldcli flags list --project <project-key> --limit 10 --offset 0 -o json

# Sort flags
ldcli flags list --project <project-key> --sort "-creationDate" -o json

# Include full targeting details (prerequisites, targets, rules)
ldcli flags list --project <project-key> --env production --summary 0 -o json
```

## Get a Single Flag

```bash
# Get a flag (all environments)
ldcli flags get --project <project-key> --flag <flag-key> -o json

# Get a flag filtered to specific environment (recommended - much smaller response)
ldcli flags get --project <project-key> --flag <flag-key> --env <env-key> -o json
```

## Create a Flag

```bash
# Create a boolean flag
ldcli flags create --project <project-key> -o json -d '{
  "name": "My New Flag",
  "key": "my-new-flag",
  "kind": "boolean",
  "description": "Controls the new feature",
  "tags": ["team-platform"],
  "variations": [
    {"value": true, "name": "Enabled"},
    {"value": false, "name": "Disabled"}
  ],
  "defaults": {
    "onVariation": 0,
    "offVariation": 1
  }
}'

# Create a multivariate string flag
ldcli flags create --project <project-key> -o json -d '{
  "name": "Banner Color",
  "key": "banner-color",
  "kind": "multivariate",
  "description": "Controls banner color",
  "variations": [
    {"value": "red", "name": "Red"},
    {"value": "blue", "name": "Blue"},
    {"value": "green", "name": "Green"}
  ],
  "defaults": {
    "onVariation": 0,
    "offVariation": 2
  }
}'

# Create a JSON flag
ldcli flags create --project <project-key> -o json -d '{
  "name": "Feature Config",
  "key": "feature-config",
  "kind": "multivariate",
  "variations": [
    {"value": {"enabled": true, "limit": 100}, "name": "Full"},
    {"value": {"enabled": false, "limit": 0}, "name": "Off"}
  ],
  "defaults": {
    "onVariation": 0,
    "offVariation": 1
  }
}'

# Clone an existing flag
ldcli flags create --project <project-key> --clone <source-flag-key> -o json -d '{
  "name": "Cloned Flag",
  "key": "cloned-flag"
}'

# Create a temporary flag (marks it for cleanup)
ldcli flags create --project <project-key> -o json -d '{
  "name": "Temporary Experiment",
  "key": "temp-experiment",
  "kind": "boolean",
  "temporary": true,
  "variations": [
    {"value": true},
    {"value": false}
  ],
  "defaults": {
    "onVariation": 0,
    "offVariation": 1
  }
}'
```

## Toggle a Flag On/Off

```bash
# Turn a flag ON in an environment
ldcli flags toggle-on --project <project-key> --flag <flag-key> --environment <env-key>

# Turn a flag OFF in an environment
ldcli flags toggle-off --project <project-key> --flag <flag-key> --environment <env-key>
```

## Archive a Flag

Archives a flag across all environments. Archived flags don't appear in the default list.

```bash
ldcli flags archive --project <project-key> --flag <flag-key>
```

## Delete a Flag

Permanently deletes a flag in all environments. Use with caution.

```bash
ldcli flags delete --project <project-key> --flag <flag-key>
```

## Get Flag Status

```bash
# Status in a specific environment
ldcli flags get-status --project <project-key> --flag <flag-key> --environment <env-key> -o json

# Status across all environments
ldcli flags get-status-across-environments --project <project-key> --flag <flag-key> -o json
```

## Common Workflows

### Create and immediately enable a boolean flag

```bash
# 1. Create the flag
ldcli flags create --project my-project -o json -d '{
  "name": "New Feature",
  "key": "new-feature",
  "kind": "boolean",
  "variations": [{"value": true}, {"value": false}],
  "defaults": {"onVariation": 0, "offVariation": 1}
}'

# 2. Toggle it on in the desired environment
ldcli flags toggle-on --project my-project --flag new-feature --environment production
```

### Find and clean up stale flags

```bash
# List archived flags
ldcli flags list --project my-project --filter "state:archived" -o json

# List flags not evaluated recently (requires filterEnv)
ldcli flags list --project my-project --filter 'evaluated:{"after":1690000000000},filterEnv:production' -o json
```

## Notes

- Flag keys must be unique within a project and can contain only lowercase letters, numbers, periods, and hyphens.
- `kind` is either `"boolean"` or `"multivariate"` (for string, number, or JSON variations).
- Newly created flags default to OFF in all environments.
- The `--env` filter on `get`/`list` significantly reduces response size and is recommended.
