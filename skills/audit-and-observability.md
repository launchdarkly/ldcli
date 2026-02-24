# Skill: Audit and Observability

Query audit logs to track changes, search for contexts, and manage metrics for experiments.

## Audit Log

The audit log records every change made in LaunchDarkly.

### List Audit Log Entries

```bash
# List recent entries
ldcli audit-log list -o json

# Get a specific entry
ldcli audit-log get --id <audit-log-entry-id> -o json
```

## Contexts

Contexts represent the entities (users, services, devices, etc.) that encounter feature flags.

### Search for Contexts

```bash
# Search contexts in a project/environment
ldcli contexts search --project <project-key> --environment <env-key> -o json -d '{
  "filter": "kind equals \"user\""
}'

# Search by key prefix
ldcli contexts search --project <project-key> --environment <env-key> -o json -d '{
  "filter": "key startsWith \"user-123\""
}'

# Search by attribute
ldcli contexts search --project <project-key> --environment <env-key> -o json -d '{
  "filter": "user.email equals \"alice@example.com\""
}'

# Fuzzy search across attribute values
ldcli contexts search --project <project-key> --environment <env-key> -o json -d '{
  "filter": "q equals \"alice\""
}'
```

### List Contexts

```bash
ldcli contexts list --project <project-key> --environment <env-key> --kind <kind> -o json
```

### Context Attribute Names and Values

```bash
# List attribute names for a context kind
ldcli contexts list-attribute-names --project <project-key> --environment <env-key> -o json

# List values for a specific attribute
ldcli contexts list-attribute-values --project <project-key> --environment <env-key> --attribute-name <attr> -o json
```

### Context Kinds

```bash
# List context kinds
ldcli contexts list-kinds-key --project <project-key> -o json

# Create or update a context kind
ldcli contexts replace-kind --project <project-key> --key <kind-key> -o json -d '{
  "name": "Organization",
  "description": "Business organizations"
}'
```

### Evaluate Flags for a Context

```bash
ldcli contexts evaluate-instance --project <project-key> --environment <env-key> --id <context-instance-id> -o json
```

### Delete Context Instances

```bash
ldcli contexts delete-instances --project <project-key> --environment <env-key> --id <context-instance-id>
```

## Metrics

Metrics track flag behavior for experiments and observability.

### List Metrics

```bash
ldcli metrics list --project <project-key> -o json
```

### Get a Metric

```bash
ldcli metrics get --project <project-key> --metric <metric-key> -o json
```

### Create a Metric

```bash
# Create a custom conversion metric
ldcli metrics create --project <project-key> -o json -d '{
  "name": "Button Clicks",
  "key": "button-clicks",
  "kind": "custom",
  "eventKey": "button-click",
  "isNumeric": false,
  "description": "Tracks button click events"
}'

# Create a custom numeric metric
ldcli metrics create --project <project-key> -o json -d '{
  "name": "Revenue",
  "key": "revenue",
  "kind": "custom",
  "eventKey": "purchase",
  "isNumeric": true,
  "unit": "USD",
  "description": "Tracks purchase revenue"
}'

# Create a page view metric
ldcli metrics create --project <project-key> -o json -d '{
  "name": "Checkout Page Views",
  "key": "checkout-views",
  "kind": "pageview",
  "urls": [{"kind": "substring", "substring": "/checkout"}],
  "description": "Tracks visits to checkout pages"
}'
```

### Delete a Metric

```bash
ldcli metrics delete --project <project-key> --metric <metric-key>
```

## Experiments (Beta)

```bash
# List experiments
ldcli experiments-beta list --project <project-key> --environment <env-key> -o json

# Get an experiment
ldcli experiments-beta get --project <project-key> --environment <env-key> --experiment <experiment-key> -o json

# Create an experiment
ldcli experiments-beta create --project <project-key> --environment <env-key> -o json -d '{
  "name": "Checkout Button Test",
  "key": "checkout-button-test",
  "maintainerId": "<member-id>",
  "iteration": {
    "hypothesis": "A green button will increase conversions",
    "metrics": [{"key": "button-clicks", "isGroup": false}],
    "treatments": [
      {"name": "Control", "baseline": true, "allocationPercent": "50", "flagKey": "checkout-button-color", "variationId": "<variation-id>"},
      {"name": "Treatment", "baseline": false, "allocationPercent": "50", "flagKey": "checkout-button-color", "variationId": "<variation-id>"}
    ],
    "flags": {"checkout-button-color": {"ruleId": "fallthrough", "flagConfigVersion": 1}}
  }
}'
```

## Filter Syntax Reference

For context searches, filters use this syntax:

| Operator | Example |
|----------|---------|
| `equals` | `kind equals "user"` |
| `notEquals` | `kind notEquals "device"` |
| `anyOf` | `kind anyOf ["user", "device"]` |
| `startsWith` | `key startsWith "user-"` |
| `contains` | `kinds contains ["user"]` |
| `exists` | `*.name exists true` |
| `before` | `myField before "2024-01-01T00:00:00Z"` |
| `after` | `myField after "2024-01-01T00:00:00Z"` |

Combine with `,` (AND), `|` (OR), and `()` (grouping).

## Notes

- Contexts are scoped to a project and environment.
- The audit log entry ID is the `_id` field in list responses.
- Metric keys and event keys are different: the metric key identifies the metric in the API, while the event key is what your SDK sends.
- Experiments are a beta feature and use the `experiments-beta` command.
