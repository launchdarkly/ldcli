## Dependabot upgrade check: #N

**Packages:** name old → new (patch, minor, or major), for every package the
manifests change, not only the ones in the title
**Check run:** CLI_SMOKE, STORE_SMOKE, and so on, or none for a superseded PR
**Tested against:** the PR branch as-is, or the PR merged onto main at <sha>
(N commits newer than the main the PR's CI ran against)
**Verdict:** merge-ok, ci-sufficient, superseded, hold, blocked, or escalate
**Escalation reasons:** none, or a list

### What changed
One or two sentences on the upgrade, including anything notable in the release
notes or advisory.

### Where ldcli uses it
File paths.

### What CI covered, and what this check added
One or two sentences each. If this check added nothing CI hadn't already shown,
say that plainly.

### What I ran
Each command or test, and whether it passed. A video link, or the reason there
isn't one.

### Other open Dependabot PRs
One line per related PR: its number, what it shares with this one, and what
the maintainer should do about it (close it once this merges, rebase it after,
merge it instead, or plan one upgrade for everything blocked by the same
package). Or "No other open Dependabot PR changes the same packages or files."

### Still unverified
Anything you couldn't test, such as a real account, a real terminal, or CGO.
