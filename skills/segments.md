# Skill: Segments

> **Requires `ldcli`.** Run `which ldcli` to check. If missing, use `/ld-setup` for install and auth instructions.

Manage audience segments. Segments are reusable groups of contexts that can be referenced in flag targeting rules. They are scoped to a project and environment.

## List Segments

```bash
ldcli segments list --project <project-key> --environment <env-key> -o json
```

## Get a Segment

```bash
ldcli segments get --project <project-key> --environment <env-key> --segment <segment-key> -o json
```

## Create a Segment

```bash
# Create a rule-based segment
ldcli segments create --project <project-key> --environment <env-key> -o json -d '{
  "name": "Beta Users",
  "key": "beta-users",
  "description": "Users opted into the beta program",
  "tags": ["beta"]
}'

# Create a segment with included/excluded keys
ldcli segments create --project <project-key> --environment <env-key> -o json -d '{
  "name": "VIP Customers",
  "key": "vip-customers",
  "included": ["user-key-1", "user-key-2"],
  "excluded": ["user-key-3"]
}'
```

## Update a Segment

Uses JSON Patch or semantic patch. Semantic patch is recommended:

```bash
# Add individual targets to a segment
ldcli segments update --project <project-key> --environment <env-key> --segment <segment-key> --semantic-patch -o json -d '{
  "instructions": [{
    "kind": "addIncludedTargets",
    "contextKind": "user",
    "values": ["user-key-4", "user-key-5"]
  }]
}'

# Remove individual targets
ldcli segments update --project <project-key> --environment <env-key> --segment <segment-key> --semantic-patch -o json -d '{
  "instructions": [{
    "kind": "removeIncludedTargets",
    "contextKind": "user",
    "values": ["user-key-4"]
  }]
}'

# Add excluded targets
ldcli segments update --project <project-key> --environment <env-key> --segment <segment-key> --semantic-patch -o json -d '{
  "instructions": [{
    "kind": "addExcludedTargets",
    "contextKind": "user",
    "values": ["user-key-6"]
  }]
}'

# Add a targeting rule to the segment
ldcli segments update --project <project-key> --environment <env-key> --segment <segment-key> --semantic-patch -o json -d '{
  "instructions": [{
    "kind": "addRule",
    "clauses": [{
      "contextKind": "user",
      "attribute": "email",
      "op": "endsWith",
      "negate": false,
      "values": ["@example.com"]
    }]
  }]
}'
```

## Delete a Segment

```bash
ldcli segments delete --project <project-key> --environment <env-key> --segment <segment-key>
```

## Check Segment Membership

```bash
# Check if a specific context is in a big segment
ldcli segments get-membership-for-context \
  --project <project-key> \
  --environment <env-key> \
  --segment <segment-key> \
  --context <context-key> \
  -o json
```

## Using Segments in Flag Targeting

Segments are referenced in flag targeting rules via the `segmentMatch` operator. See [flag-targeting.md](./flag-targeting.md):

```bash
ldcli flags update --project <project-key> --flag <flag-key> --semantic-patch -o json -d '{
  "environmentKey": "production",
  "instructions": [{
    "kind": "addRule",
    "variationId": "<variation-id>",
    "clauses": [{
      "contextKind": "user",
      "attribute": "",
      "op": "segmentMatch",
      "negate": false,
      "values": ["beta-users"]
    }]
  }]
}'
```

## Notes

- Segments are scoped to a specific project AND environment.
- A "big segment" is a synced segment or a list-based segment with more than 15,000 entries.
- Segment keys must be unique within a project/environment pair.
- Use segments to DRY up flag targeting: define the audience once, reference it in many flags.
