# Skill: Flag Targeting

> **Prerequisites:** This skill requires `ldcli`. Before running any command below, verify it's available (`which ldcli`). If not found, offer to install it:
> - macOS: `brew tap launchdarkly/homebrew-tap && brew install ldcli`
> - npm: `npm install -g @launchdarkly/ldcli`
> - Binary downloads: https://github.com/launchdarkly/ldcli/releases
>
> After install, authenticate with `ldcli login` or by setting `LD_ACCESS_TOKEN`. Use `-o json` on all commands for parseable output.

Update flag targeting rules, percentage rollouts, individual user/context targets, and default variations using semantic patch.

## How Flag Updates Work

`ldcli flags update` supports three patch formats. **Semantic patch is recommended** for agents because it uses readable, intent-based instructions rather than JSON pointer paths.

```bash
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json \
  -d '<semantic-patch-json>'
```

A semantic patch body has this structure:

```json
{
  "comment": "Optional description of the change",
  "environmentKey": "environment-key",
  "instructions": [
    { "kind": "instructionName", ...params }
  ]
}
```

**Important**: To find variation IDs and rule IDs needed for targeting, first get the flag:

```bash
ldcli flags get --project <project-key> --flag <flag-key> --env <env-key> -o json
```

The response contains:
- `variations[].\_id` - variation IDs
- `environments.<env>.rules[].\_id` - rule IDs
- `environments.<env>.rules[].clauses[].\_id` - clause IDs

## Toggle Flag On/Off

The simplest way:

```bash
ldcli flags toggle-on --project <project-key> --flag <flag-key> --environment <env-key>
ldcli flags toggle-off --project <project-key> --flag <flag-key> --environment <env-key>
```

Or via semantic patch:

```bash
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{ "kind": "turnFlagOn" }]
}'
```

## Individual Context Targeting

Add specific contexts to receive a particular variation:

```bash
# Add individual targets
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "addTargets",
    "contextKind": "user",
    "variationId": "<variation-id>",
    "values": ["user-key-1", "user-key-2"]
  }]
}'

# Remove individual targets
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "removeTargets",
    "contextKind": "user",
    "variationId": "<variation-id>",
    "values": ["user-key-1"]
  }]
}'
```

## Add a Targeting Rule

Targeting rules let you serve specific variations based on context attributes:

```bash
# Serve a variation to users in specific countries
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "addRule",
    "variationId": "<variation-id>",
    "clauses": [{
      "contextKind": "user",
      "attribute": "country",
      "op": "in",
      "negate": false,
      "values": ["US", "CA"]
    }]
  }]
}'

# Serve a percentage rollout based on a rule
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "addRule",
    "clauses": [{
      "contextKind": "user",
      "attribute": "plan",
      "op": "in",
      "negate": false,
      "values": ["enterprise"]
    }],
    "rolloutContextKind": "user",
    "rolloutWeights": {
      "<true-variation-id>": 50000,
      "<false-variation-id>": 50000
    }
  }]
}'
```

### Clause Operators

| Operator | Description |
|----------|-------------|
| `in` | Matches if attribute value is in the list |
| `endsWith` | String ends with |
| `startsWith` | String starts with |
| `matches` | Regex match |
| `contains` | String contains |
| `lessThan` | Numeric less than |
| `lessThanOrEqual` | Numeric less than or equal |
| `greaterThan` | Numeric greater than |
| `greaterThanOrEqual` | Numeric greater than or equal |
| `before` | Date is before |
| `after` | Date is after |
| `segmentMatch` | Context is in a segment |
| `semVerEqual` | Semantic version equals |
| `semVerLessThan` | Semantic version less than |
| `semVerGreaterThan` | Semantic version greater than |

Set `"negate": true` to invert any operator.

## Modify Existing Rules

```bash
# Add clauses to an existing rule
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "addClauses",
    "ruleId": "<rule-id>",
    "clauses": [{
      "contextKind": "user",
      "attribute": "email",
      "op": "endsWith",
      "negate": false,
      "values": ["@example.com"]
    }]
  }]
}'

# Remove a rule
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "removeRule",
    "ruleId": "<rule-id>"
  }]
}'

# Reorder rules
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "reorderRules",
    "ruleIds": ["<rule-id-1>", "<rule-id-2>", "<rule-id-3>"]
  }]
}'
```

## Change Default Variations

The "fallthrough" is what's served when targeting is ON but no rules match:

```bash
# Set fallthrough to a specific variation
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "updateFallthroughVariationOrRollout",
    "variationId": "<variation-id>"
  }]
}'

# Set fallthrough to a percentage rollout
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "updateFallthroughVariationOrRollout",
    "rolloutWeights": {
      "<variation-id-true>": 20000,
      "<variation-id-false>": 80000
    },
    "rolloutContextKind": "user"
  }]
}'

# Change the off variation (served when flag is OFF)
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "updateOffVariation",
    "variationId": "<variation-id>"
  }]
}'
```

## Add Prerequisites

Require another flag to be serving a specific variation before this flag evaluates:

```bash
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "addPrerequisite",
    "key": "prerequisite-flag-key",
    "variationId": "<variation-id-of-prerequisite>"
  }]
}'
```

## Multiple Instructions in One Patch

You can combine multiple instructions in a single update:

```bash
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "comment": "Enable flag with 20% rollout for beta users",
  "environmentKey": "production",
  "instructions": [
    { "kind": "turnFlagOn" },
    {
      "kind": "addRule",
      "variationId": "<variation-id>",
      "clauses": [{
        "contextKind": "user",
        "attribute": "beta",
        "op": "in",
        "negate": false,
        "values": [true]
      }]
    }
  ]
}'
```

## Notes

- Rollout weights are in thousandths of a percent (0-100000). So 50000 = 50%.
- All rollout weights for a rule must sum to 100000.
- When adding individual targets, a context key cannot appear in multiple variations.
- Rule evaluation is ordered: the first matching rule wins. Use `reorderRules` to change priority.
- Semantic patch requires `--semantic-patch` flag on the CLI command.
