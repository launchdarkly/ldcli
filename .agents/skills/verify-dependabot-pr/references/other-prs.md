# Other open Dependabot PRs

`scripts/other-prs.sh <pr-number>` prints three blocks: what the PR you're
verifying changes, what every other open Dependabot PR changes, and which of
them overlap with yours. Each change shows the old version, the new version,
and what `main` has now.

The script reads manifests, not titles: `go.mod`, `package.json`, the
top-level entries in `package-lock.json` (marked `lock`), `uses:` lines under
`.github/`, and `FROM` lines in Dockerfiles. It doesn't list nested
`package-lock.json` entries, so a package that's only installed as a nested
copy won't show up. Search the lockfile if you need to know about one.

## What to look for

Mention each of these in the report's "Other open Dependabot PRs" section when
you find it. They're for the maintainer; none of them changes which check you
run, except where noted.

- **This PR is `SUPERSEDED`.** `main` already has every version it moves to,
  usually because a later PR or a manual upgrade got there first. The verdict
  is superseded. Recommend closing it and skip the checks.
- **`main already has N of M changes`.** Common on old PRs whose `go.mod` also
  moved `golang.org/x/*` modules that `main` has since passed. Only the
  remaining changes matter; test those, and say that a rebase or
  `@dependabot recreate` would shrink the PR to them.
- **This PR goes further than another one on the same package.** The other PR
  can be closed once this one merges. Name it.
- **Another PR goes further on the same package.** Say so. If the other PR can
  land, this one is redundant; if the other one is blocked, this one may still
  be worth merging on its own.
- **One PR removes a package another one bumps.** For example, a component
  library upgrade can drop the dependency a security PR bumps. Merging the
  upgrade would resolve the security PR too, so say which PR makes the other
  unnecessary.
- **A grouped or security PR changes packages that separate PRs also change.**
  List those PRs. Whichever lands first changes what the others need.
- **Two PRs only share files**, such as `go.mod` and `go.sum` or one
  `package-lock.json`. They don't conflict about versions, but whichever
  merges first can make the other conflict. Dependabot rebases its own PRs
  automatically, except PRs that have been open for more than 30 days. Say
  which of the overlapping PRs are that old, because they'll need a
  `@dependabot rebase` comment.
- **Your verdict is blocked.** Look through the output for other PRs that move
  the same package family or depend on the same blocker, such as several PRs
  that all need a newer React. List them together so a maintainer can plan one
  upgrade that unblocks all of them. You don't need to check them yourself.

If nothing overlaps, write "No other open Dependabot PR changes the same
packages or files."
