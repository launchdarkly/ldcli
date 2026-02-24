# Skill: Members and Teams

> **Prerequisites:** This skill requires `ldcli`. Before running any command below, verify it's available (`which ldcli`). If not found, offer to install it:
> - macOS: `brew tap launchdarkly/homebrew-tap && brew install ldcli`
> - npm: `npm install -g @launchdarkly/ldcli`
> - Binary downloads: https://github.com/launchdarkly/ldcli/releases
>
> After install, authenticate with `ldcli login` or by setting `LD_ACCESS_TOKEN`. Use `-o json` on all commands for parseable output.

Manage account members, invite new users, and organize them into teams with custom roles.

## Members

### List Members

```bash
ldcli members list -o json
```

### Get a Member

```bash
ldcli members get --id <member-id> -o json
```

### Invite New Members

```bash
# Invite with default role (reader)
ldcli members invite --emails "alice@example.com,bob@example.com"

# Invite with a specific role
ldcli members invite --emails "carol@example.com" --role writer

# Available built-in roles: reader, writer, admin
```

### Update a Member

```bash
ldcli members update --id <member-id> -o json -d '[
  {"op": "replace", "path": "/role", "value": "writer"}
]'
```

### Delete a Member

```bash
ldcli members delete --id <member-id>
```

## Teams (Enterprise)

### List Teams

```bash
ldcli teams list -o json
```

### Get a Team

```bash
ldcli teams get --team <team-key> -o json
```

### Create a Team

```bash
ldcli teams create -o json -d '{
  "name": "Platform Team",
  "key": "platform-team",
  "description": "Manages platform infrastructure flags"
}'
```

### Add Members to a Team

```bash
ldcli teams create-members --team <team-key> -o json -d '[
  {"memberId": "<member-id-1>"},
  {"memberId": "<member-id-2>"}
]'
```

### List Team Maintainers and Roles

```bash
ldcli teams list-maintainers --team <team-key> -o json
ldcli teams list-roles --team <team-key> -o json
```

### Update a Team

```bash
ldcli teams update --team <team-key> -o json --semantic-patch -d '{
  "instructions": [{
    "kind": "addCustomRoles",
    "values": ["<custom-role-key>"]
  }]
}'
```

### Delete a Team

```bash
ldcli teams delete --team <team-key>
```

## Custom Roles (Enterprise)

### List Custom Roles

```bash
ldcli custom-roles list -o json
```

### Get a Custom Role

```bash
ldcli custom-roles get --id <role-id> -o json
```

### Create a Custom Role

```bash
ldcli custom-roles create -o json -d '{
  "name": "Flag Manager",
  "key": "flag-manager",
  "description": "Can manage flags but not projects",
  "policy": [
    {
      "effect": "allow",
      "actions": ["*"],
      "resources": ["proj/*:env/*:flag/*"]
    },
    {
      "effect": "deny",
      "actions": ["deleteProject"],
      "resources": ["proj/*"]
    }
  ]
}'
```

### Delete a Custom Role

```bash
ldcli custom-roles delete --id <role-id>
```

## Common Workflows

### Invite a new team member with a custom role

```bash
# 1. Invite the member
ldcli members invite --emails "newuser@example.com" --role reader

# 2. Find their member ID
ldcli members list -o json
# Look for the member in the response

# 3. Add them to a team (which may grant additional roles)
ldcli teams create-members --team platform-team -o json -d '[
  {"memberId": "<member-id>"}
]'
```

## Notes

- Member IDs are returned in the `_id` field of member responses.
- Teams and custom roles are Enterprise features.
- Built-in roles: `reader` (view only), `writer` (can modify), `admin` (full access).
- Custom role policies use a resource specifier syntax: `proj/<key>:env/<key>:flag/<key>`.
