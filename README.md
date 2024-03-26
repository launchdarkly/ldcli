# ldcli

## Commands

`go run main.go setup` - runs the setup wizard to create a feature flag for a project and environment.

### Examples

Enable a flag with key `test-flag` in project `default` and environment `production`.
```
go run main.go flags update \
    -t {accessToken} \
    -u http://localhost:3000 \
    -d '[{"op": "replace", "path": "/environments/production/on", "value": true}]' \
    --projKey default \
    --key test-flag
```

## Running Github Actions locally

To run the Github Actions locally, you need to install the `act` tool.
```bash
brew install act
```
You will also want to have the `gh` tool installed and authenticated.
```bash
brew install gh
gh auth login
```

Example of how to run the `release-please` action locally.
`-s` flag is to pass in secrets (optional)
`-j` flag is to specify the job to run (optional)
`-W` flag is to specify the workflow file to run (optional)
```bash
act -s GITHUB_TOKEN="$(gh auth token)" -j release-please -W ./.github/workflows/release-please.yml
```
## Submitting bug reports and feature requests

The LaunchDarkly team monitors this repository's [issue tracker](https://github.com/launchdarkly/ldcli/issues). Use the issue tracker to file bug reports and feature requests.

## Submitting pull requests

We encourage pull requests and other contributions from the community.

Before you submit pull requests, make sure that all temporary or unintended code is removed. Don't worry about adding reviewers to the pull request. The LaunchDarkly team will add themselves.
