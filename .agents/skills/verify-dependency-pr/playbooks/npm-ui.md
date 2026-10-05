# Playbook: dev-server UI (`internal/dev_server/ui`, npm)

## Baseline (from `verify.sh`)

`ui-npm-ci` (detects ERESOLVE), `ui-lint`, `ui-prettier`, `ui-test`, `ui-build-drift`, `ui-npm-ls`, `ui-npm-audit` (compared with base), `ui-dep-usage`, and then `binary-smoke`. The smoke test embeds the **rebuilt** `dist/`, so it checks what main would serve once dist is rebuilt.

## What usually happens

- **Stale `dist/`.** Any runtime dependency change rebuilds `dist/index.html` (a single-file Vite build embedded through `go:embed`). Dependabot never commits it, so the `dev-server UI` CI job fails. `ui-build-drift` then recommends a rebuild. That is "needs human" (someone pushes the rebuild), not a block.
- **ERESOLVE.** `@launchpad-ui/*`, React 18, and the `react-router-dom` v6 `overrides` conflict with each other (#638, #642, #723, #779). If `npm ci` fails, the verdict is block. Recommend a coordinated upgrade or a scoped `overrides` entry (#777 shows the pattern), or closing the PR in favor of a focused one.
- **Unused dependency.** `react-window` is not imported anywhere (#831). `ui-dep-usage` flags it, and the right fix is removing the dependency rather than bumping it.

## Impact analysis

- Read the release notes for majors: React or react-router majors need peer and Node upgrades (react-router 8 needs React ≥19.2.7 and Node ≥22.22, per Bugbot on #729). The `ui-dep-usage` details list the `engines` and `peerDependencies` of each updated package.
- Find usage: `rg "from '<pkg>" internal/dev_server/ui/src`, and for build tooling, `vite.config.ts`, `vitest.config.ts`, and `eslint.config.js`.
- devDependencies (eslint, prettier, vitest, typescript, vite plugins) don't ship code, but they can reformat files (prettier), change lint rules, or change the built `dist/` (vite, typescript). Check `ui-prettier` and `ui-build-drift`.
- Security bumps: check which advisory is fixed (`ui-npm-audit` lists the advisories the PR resolves) and whether a smaller bump would do (the minimum patched version, as in #740 and #777).

## Generated-check ideas

| Update | Discriminating / guard idea |
|---|---|
| A component library (`@launchpad-ui/*`) | Guard: a vitest render test of the screens that use the changed components, in a temporary `src/__verify__/*.test.tsx`. Run `npx vitest run src/__verify__` and delete the file afterwards. |
| `launchdarkly-js-client-sdk` | Start the dev server, load `/ui/`, and check that the bundle initializes the SDK against the dev server's `/sdk` endpoints. |
| `vite` / plugins / typescript | Guard: the built `dist/index.html` is a single file (no external `<script src=`), has `<div id="root">`, and its size stays within ±20% of base. |
| A security fix | Discriminating: reproduce the advisory's vulnerable call path, if ldcli reaches it. Otherwise state in `impact.json` that it is unreachable. |
| Unused dependency | No check needed. Recommend removing it. |
