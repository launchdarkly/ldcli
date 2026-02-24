# Skill: Projects and Environments

Manage LaunchDarkly projects and their environments. Projects contain feature flags; environments (e.g., production, staging, development) contain the flag configurations and targeting rules.

## Projects

### List Projects

```bash
# List all projects
ldcli projects list -o json

# Search by name or key
ldcli projects list --filter "query:my-project" -o json

# Include environments in the response
ldcli projects list --expand "environments" -o json

# Paginate
ldcli projects list --limit 10 --offset 0 -o json

# Sort
ldcli projects list --sort "name" -o json
ldcli projects list --sort "-createdOn" -o json   # descending
```

### Get a Project

```bash
ldcli projects get --project <project-key> -o json
```

### Create a Project

```bash
ldcli projects create -o json -d '{
  "name": "My Project",
  "key": "my-project",
  "tags": ["team-platform"]
}'

# Create with custom default environments
ldcli projects create -o json -d '{
  "name": "My Project",
  "key": "my-project",
  "environments": [
    {"name": "Development", "key": "development", "color": "339933"},
    {"name": "Staging", "key": "staging", "color": "FF9900"},
    {"name": "Production", "key": "production", "color": "CC0000"}
  ]
}'
```

### Update a Project

```bash
# Uses JSON Patch format
ldcli projects update --project <project-key> -o json -d '[
  {"op": "replace", "path": "/name", "value": "Updated Project Name"},
  {"op": "replace", "path": "/tags", "value": ["new-tag"]}
]'
```

### Delete a Project

```bash
ldcli projects delete --project <project-key>
```

### Flag Defaults

```bash
# Get flag defaults for a project
ldcli projects get-flag-defaults --project <project-key> -o json

# Update flag defaults
ldcli projects update-flag-defaults --project <project-key> -o json -d '[
  {"op": "replace", "path": "/defaultClientSideAvailability/usingMobileKey", "value": true}
]'
```

## Environments

### List Environments

```bash
ldcli environments list --project <project-key> -o json
```

### Get an Environment

```bash
ldcli environments get --project <project-key> --environment <env-key> -o json
```

### Create an Environment

```bash
ldcli environments create --project <project-key> -o json -d '{
  "name": "Staging",
  "key": "staging",
  "color": "FF9900"
}'

# With additional options
ldcli environments create --project <project-key> -o json -d '{
  "name": "Production",
  "key": "production",
  "color": "CC0000",
  "defaultTtl": 60,
  "secureMode": true,
  "confirmChanges": true,
  "requireComments": true,
  "tags": ["critical"]
}'
```

### Update an Environment

```bash
# Uses JSON Patch format
ldcli environments update --project <project-key> --environment <env-key> -o json -d '[
  {"op": "replace", "path": "/name", "value": "New Environment Name"},
  {"op": "replace", "path": "/color", "value": "0000CC"}
]'
```

### Delete an Environment

```bash
ldcli environments delete --project <project-key> --environment <env-key>
```

### Reset SDK Keys

```bash
# Reset server-side SDK key
ldcli environments reset-sdk-key --project <project-key> --environment <env-key> -o json

# Reset mobile SDK key
ldcli environments reset-mobile-key --project <project-key> --environment <env-key> -o json
```

## Common Workflows

### Set up a new project with standard environments

```bash
# Create the project with environments
ldcli projects create -o json -d '{
  "name": "Payment Service",
  "key": "payment-service",
  "tags": ["backend"],
  "environments": [
    {"name": "Development", "key": "dev", "color": "339933"},
    {"name": "QA", "key": "qa", "color": "0066FF"},
    {"name": "Staging", "key": "staging", "color": "FF9900"},
    {"name": "Production", "key": "production", "color": "CC0000", "confirmChanges": true, "requireComments": true}
  ]
}'
```

### Discover project keys

When you don't know the project key, list projects first:

```bash
ldcli projects list -o json
```

Then use the `key` field from the response.

## Notes

- Project keys must be unique within an account.
- Environment keys must be unique within a project.
- Color is a 6-character hex string (no `#` prefix).
- `defaultTtl` is in minutes: how long the SDK can cache flag values.
- `secureMode` ensures context keys are not exposed client-side (requires SDK support).
- `confirmChanges` and `requireComments` enforce a safety workflow in the UI.
