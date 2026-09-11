# Skill: LaunchDarkly CLI Setup

Install and configure `ldcli`, the LaunchDarkly CLI.

## Check if ldcli is installed

```bash
which ldcli && ldcli --version
```

If `ldcli` is not found, help the user install it using one of the methods below.

## Install ldcli

### macOS (Homebrew)

```bash
brew tap launchdarkly/homebrew-tap
brew install ldcli
```

### npm

```bash
npm install -g @launchdarkly/ldcli
```

### Docker

```bash
docker pull launchdarkly/ldcli
```

### Binary downloads (Linux/Windows)

Download the latest release from https://github.com/launchdarkly/ldcli/releases

## Authenticate

After installing, the user must authenticate. There are two options:

### Interactive login (recommended)

```bash
ldcli login
```

This opens a browser for OAuth authentication and saves the token to the local config.

### Access token

Set an API access token as an environment variable:

```bash
export LD_ACCESS_TOKEN="<your-token>"
```

Or pass it per-command with `--access-token <token>`.

## Verify

```bash
ldcli projects list -o json
```

If this returns a list of projects, everything is working.

## Notes

- Always use `-o json` on commands for parseable output.
- Use `ldcli --help` or `ldcli <command> --help` to explore available commands.
- See `ldcli resources` for a full list of resource types you can manage.
- Configuration is stored in `~/.config/ldcli/config.yml` (XDG standard path).
