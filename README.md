# LD-CLI

## Commands

`go run main.go setup` - runs the setup wizard to create a feature flag for a project and environment.

## Running Github Actions locally

To run the Github Actions locally, you need to install the `act` tool.
```bash
brew install act
```

Then you can run the Github Actions locally by running the following command.
```bash
act -s GITHUB_TOKEN="$(gh auth token)"
```

Note: You need to have the `gh` tool installed and authenticated.

Example of how to run the `release-please` action locally.
```bash
act -s GITHUB_TOKEN="$(gh auth token)" -j release-please -W ./.github/workflows/release-please.yml
```