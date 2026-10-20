#!/usr/bin/env bash
# `npm publish` runs in the repository root, and .npmignore lists what to
# leave out. A new top-level directory goes into the package unless someone
# adds it to .npmignore. The package needs only package.json, README, and
# LICENSE: its postinstall downloads the binary from the GitHub release.
source "$VERIFY_ROOT/lib/check.sh"

pack() {
  (cd "$1" && npm pack --dry-run --json --ignore-scripts 2>"$ARTIFACTS/$2.err") >"$ARTIFACTS/$2.json" || return 1
  jq -r '.[0].files[].path' "$ARTIFACTS/$2.json" | sort >"$ARTIFACTS/$2.files"
}
pack "$BASE_WT" base || { cat "$ARTIFACTS/base.err"; incomplete "npm pack failed on the last release"; }
pack "$PR_WT" pr || { cat "$ARTIFACTS/pr.err"; incomplete "npm pack failed on the candidate"; }

comm -13 "$ARTIFACTS/base.files" "$ARTIFACTS/pr.files" >"$ARTIFACTS/added.files"
comm -23 "$ARTIFACTS/base.files" "$ARTIFACTS/pr.files" >"$ARTIFACTS/removed.files"
size() { jq -r '.[0] | "\(.entryCount) files, \(.unpackedSize / 1024 | floor) KB unpacked"' "$ARTIFACTS/$1.json"; }
detail "- Last release: $(size base). Candidate: $(size pr)."

# Files that must never be published, and files that the package does not need.
SECRET_RE='(^|/)\.env|\.pem$|\.key$|\.p12$|\.db$|(^|/)\.verify-out/|(^|/)id_rsa|(^|/)\.npmrc$'
TOOLING_RE='^\.(agents|claude|cursor|codex|github|vscode|idea)/|^scripts/|^(AGENTS|CLAUDE)\.md$|^\.pre-commit|^tools\.go$|^CODEOWNERS$'
grep -E "$SECRET_RE" "$ARTIFACTS/pr.files" >"$ARTIFACTS/secret.files"
grep -E "$TOOLING_RE" "$ARTIFACTS/added.files" >"$ARTIFACTS/tooling-new.files"
grep -E "$TOOLING_RE" "$ARTIFACTS/base.files" >"$ARTIFACTS/tooling-old.files"

if [ -s "$ARTIFACTS/added.files" ]; then
  detail "- New files in the package ($(wc -l <"$ARTIFACTS/added.files")):"
  head -n 30 "$ARTIFACTS/added.files" | sed 's/^/  - `/; s/$/`/' >>"$ARTIFACTS/details.md"
fi
[ -s "$ARTIFACTS/removed.files" ] && detail "- Files no longer in the package: $(paste -sd, "$ARTIFACTS/removed.files")"
[ -s "$ARTIFACTS/tooling-old.files" ] &&
  detail "- The last release already published $(wc -l <"$ARTIFACTS/tooling-old.files") repository tooling file(s), for example \`$(head -n1 "$ARTIFACTS/tooling-old.files")\`."

if [ -s "$ARTIFACTS/secret.files" ]; then
  findings_file "$ARTIFACTS/secret.files"
  recommend "Add these paths to .npmignore: $(paste -sd' ' "$ARTIFACTS/secret.files")"
  fail "The package contains files that can hold secrets or local data: $(head -n5 "$ARTIFACTS/secret.files" | paste -sd, -)"
fi
if [ -s "$ARTIFACTS/tooling-new.files" ]; then
  dirs=$(sed -E 's#^([^/]+/).*#\1#' "$ARTIFACTS/tooling-new.files" | sort -u | paste -sd' ' -)
  findings_file "$ARTIFACTS/tooling-new.files"
  recommend "To leave them out, add these lines to .npmignore: $dirs"
  decide "Publish the npm package with $(wc -l <"$ARTIFACTS/tooling-new.files") new repository tooling file(s) that the package does not need ($dirs)?" \
    "The candidate adds them to the npm package, because .npmignore does not list them"
fi
pass "No new files in the package that it does not need ($(size pr))"
