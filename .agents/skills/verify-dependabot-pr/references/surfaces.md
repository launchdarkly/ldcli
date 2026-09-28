# Which check each dependency needs

The check depends on what kind of package it is, which rarely changes. This
table doesn't list file paths, because those do change. Find where ldcli uses
the package with a search.

If a package isn't listed, use the row for the most similar package. If your
search shows ldcli using a package differently from what its row assumes (for
example, a package listed as NO_EXTRA that ldcli now imports directly), go with
what the search found and add a line to the report saying the table needs
updating.

## Go modules

| Package | What it does in ldcli | Check |
| --- | --- | --- |
| `spf13/cobra` | The CLI framework behind every command | CLI_SMOKE |
| `spf13/pflag` | Flag parsing and wrapped help output | CLI_SMOKE |
| `spf13/viper` | Reads flags, `LD_` environment variables, and the config file | CLI_SMOKE, with one env var and one config value |
| `golang.org/x/term` | Terminal width for help, and deciding output defaults | CLI_SMOKE, both piped and in a terminal |
| `charmbracelet/bubbletea`, `bubbles`, `lipgloss` | The interactive setup and quickstart flows | CLI_SMOKE in a terminal; escalate if you can't get one |
| `charmbracelet/glamour` | Renders help text for resource commands | CLI_SMOKE, with `--help` on a few resource commands |
| `mattn/go-sqlite3` | SQLite driver for the dev-server's stored data | STORE_SMOKE |
| `gorilla/mux`, `gorilla/handlers` | Dev-server routing, CORS, and request logging | STORE_SMOKE |
| `launchdarkly/go-server-sdk`, `go-sdk-common` | The dev-server's flag data and the SDK setup commands | STORE_SMOKE and CLI_SMOKE |
| `go.uber.org/mock` | Generating test mocks | TEST_ONLY |
| `oapi-codegen`, `getkin/kin-openapi` | Generating the dev-server API and resource commands | BUILD_ONLY |
| `golang.org/x/net`, `x/oauth2`, `x/sys`, and other `x/*` | Usually pulled in by other packages | Search first; NO_EXTRA if ldcli doesn't import it |

## npm: the dev-server UI

| Package | What it does in ldcli | Check |
| --- | --- | --- |
| `react`, `react-dom`, `react-router` | Renders the UI and its pages | UI_COMPUTER_USE. A new `react-router` major can require a newer `react`; compare its peer requirement with `react` in `package.json` first |
| `@launchpad-ui/*` | LaunchDarkly's UI components | UI_COMPUTER_USE; look for unstyled or missing components. `core` and `components` share `react-aria` packages, so upgrading one alone can fail `npm ci`. Both declare a `react` peer requirement |
| `launchdarkly-js-client-sdk` | Only its TypeScript types (`LDFlagSet`, `LDFlagValue`); the UI never calls the SDK | BUILD_ONLY, because `npm run build` type-checks. Use UI_COMPUTER_USE if a search shows the UI calling it |
| `fuzzysort` | Flag search | UI_COMPUTER_USE, searching the flag list |
| `lodash` | `isEqual` when comparing a flag's override with its variations | UI_COMPUTER_USE, overriding a flag and resetting it |
| `react-window` | Listed in `package.json`, but not imported | NO_EXTRA, unless a search finds it imported |
| `vite`, `vite-plugin-*`, `rollup`, `typescript` | Builds the checked-in `dist/` bundle | BUILD_ONLY, and check whether `dist/` changed |
| `vitest`, `@testing-library/*` | The UI's tests. `@testing-library/react` is listed under `dependencies`, but it's only used in tests. | TEST_ONLY |
| `prettier`, `eslint`, `eslint-plugin-*`, `typescript-eslint` | Formatting and linting | BUILD_ONLY |
| Packages that only appear in the lockfile | Pulled in by other packages | NO_EXTRA, unless a search finds ldcli importing them |
| Grouped or security updates (`npm_and_yarn`, `go_modules` groups) | Can upgrade parent packages to reach a fixed version, so the PR changes more than its title says | Use the row for every direct dependency `scripts/other-prs.sh` lists for the PR, and escalate if the title doesn't name them |

## npm: the repo root

| Package | What it does in ldcli | Check |
| --- | --- | --- |
| `@go-task/go-npm` | Downloads the release binary for `npm install -g @launchdarkly/ldcli` | INSTALL_SMOKE |

## GitHub Actions and Docker

| Package | What it does in ldcli | Check |
| --- | --- | --- |
| `actions/*` | CI steps | CI_ONLY; a major bump is an escalation reason |
| `googleapis/release-please-action` | Release automation | CI_ONLY |
| `launchdarkly/gh-actions/*` | Shared LaunchDarkly CI steps | CI_ONLY |
| Base image in `Dockerfile.goreleaser` | The published Docker image | CI_ONLY |
