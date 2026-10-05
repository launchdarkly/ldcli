# Playbook: dev-server UI (`internal/dev_server/ui`, npm)

## Diligent reviewer standard

A diligent reviewer makes sure that these statements are true for a UI update:

1. `npm ci` resolves the tree with no peer conflict, and `npm ls --all` reports no new problem.
2. Lint, format, tests, and the production build pass.
3. The committed `dist/` is the same as a fresh build. The Go binary embeds `dist/`, so a stale `dist/` ships old code.
4. The binary with the new `dist/` serves the UI.
5. Each updated direct dependency is used. An unused dependency is a candidate for removal.
6. The upstream changes are known: breaking changes, new `engines` and peer requirements, and security fixes.
7. New and changed transitive packages are known. No new install script runs without approval. Licenses are in the policy.
8. No new advisory appears in the runtime dependencies.

## What the baseline checks

| Statement | Check |
|---|---|
| 1 | `ui-npm-ci` (gate), `ui-npm-ls` |
| 2 | `ui-lint`, `ui-prettier`, `ui-test` (gate), `ui-build-drift` |
| 3 | `ui-build-drift`. If it fails, `result.json` has the fix recipe `ui-dist-rebuild`. |
| 4 | `binary-smoke`. It runs after `ui-build-drift`, so it embeds the new build. |
| 5 | `ui-dep-usage`. Its details also list `engines`, peer dependencies, and deprecation notes. |
| 6 | `upstream-changes` collects the notes. The agent does the mapping. |
| 7 | `transitive-changes`, `license-changes` |
| 8 | `ui-npm-audit` |

## Known failure modes

- A runtime dependency change rebuilds `dist/index.html`. Dependabot does not commit it, so the `dev-server UI` CI job fails (#639, #640). The verdict is `block`, with the rebuild as the fix.
- `@launchpad-ui/*`, React 18, and the `react-router-dom` v6 `overrides` conflict with each other (#638, #642, #723, #779). If `npm ci` fails, the verdict is `block`. A coordinated upgrade or a scoped `overrides` entry fixes it (#777 shows the pattern).
- No code imports `react-window` (#831). `ui-dep-usage` asks if the dependency can go.

## What the agent must do

- Read the notes for each direct update. For a major update, find the removed or renamed APIs and search for them in `src/`.
- Compare the `engines` and peer requirements in the `ui-dep-usage` details with the Node version that CI uses (`lts/*`) and with React 18.
- devDependencies do not ship, but they can change the format (prettier), the lint rules, or the built `dist/` (vite, typescript). Look at `ui-prettier` and `ui-build-drift`.
- For a security update, name the advisory that the update fixes (`ui-npm-audit` lists the fixed advisories).

## Generated-check ideas

| Update | Idea |
|---|---|
| A component library (`@launchpad-ui/*`) | A guard: a vitest render test of the screens that use the changed components, in a temporary `src/__verify__/` folder. |
| `launchdarkly-js-client-sdk` | Start the dev server, load `/ui/`, and make sure that the bundle connects to the `/sdk` endpoints of the dev server. |
| `vite`, plugins, typescript | A guard: `dist/index.html` is one file with no external scripts, has `<div id="root">`, and its size is within 20% of base. |
| An unused dependency | A guard: the new build is the same, byte for byte, as the build on base. |

## When to ask a person

- An updated dependency is unused. Ask if it can go instead of the update.
- A new advisory appears in the runtime dependencies. Ask if it is acceptable, and name the advisory.
- A new install script or a license outside the policy appears.
