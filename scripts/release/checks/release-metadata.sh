#!/usr/bin/env bash
# The version must be the same in every file that release-please updates, and
# the size of the bump must match the conventional commits since the last
# release: a breaking change needs a major bump, a feature a minor bump.
source "$VERIFY_ROOT/lib/check.sh"
cd "$WT" || exit 1
: "${RELEASE_INVENTORY:?}"

version=$(jq -r '."." // empty' .release-please-manifest.json 2>/dev/null)
[ -n "$version" ] || fail ".release-please-manifest.json has no version"
base_version=$(jq -r '."." // empty' "$BASE_WT/.release-please-manifest.json" 2>/dev/null)

problems=()
pkg=$(jq -r '.version // empty' package.json)
[ "$pkg" = "$version" ] || problems+=("package.json has version '$pkg', expected $version")
prov=$(sed -n 's/^PACKAGE_VERSION=//p' PROVENANCE.md | head -n1)
[ "$prov" = "$version" ] || problems+=("PROVENANCE.md has PACKAGE_VERSION '$prov', expected $version")
grep -qE "^## \[?$(printf '%s' "$version" | sed 's/\./\\./g')\]?" CHANGELOG.md || problems+=("CHANGELOG.md has no section for $version")
detail "- Version: \`$base_version\` to \`$version\` (manifest, package.json, PROVENANCE.md, CHANGELOG.md)"

# The bump that the commits ask for. A commit and its revert in the same range cancel out.
expected=$(jq -r '
  [.commits[] | select(.release_commit | not) | select(.net_effect == null)
   | if .breaking then 3 elif .type == "feat" then 2 elif (.type == "fix" or .type == "perf" or .type == "revert") then 1 else 0 end]
  | (max // 0) | {"3": "major", "2": "minor", "1": "patch", "0": "none"}[tostring]' "$RELEASE_INVENTORY")
actual=$(python3 - "$base_version" "$version" <<'PY'
import sys
def parse(v):
    core = v.split("-")[0].split("+")[0]
    return [int(x) for x in core.split(".")]
try:
    a, b = parse(sys.argv[1]), parse(sys.argv[2])
except Exception:
    print("unknown"); sys.exit()
if b <= a: print("none")
elif b[0] > a[0]: print("major")
elif b[1] > a[1]: print("minor")
else: print("patch")
PY
)
detail "- Bump: \`$actual\`. The commits ask for \`$expected\`."
rank() { case "$1" in major) echo 3 ;; minor) echo 2 ;; patch) echo 1 ;; *) echo 0 ;; esac; }
major=${base_version%%.*}
if [ "$actual" = none ] || [ "$actual" = unknown ]; then
  problems+=("the version does not go up from $base_version to $version")
elif [ "$(rank "$actual")" -lt "$(rank "$expected")" ]; then
  # Before 1.0.0, release-please puts a breaking change in a minor bump.
  if ! { [ "$expected" = major ] && [ "$major" = 0 ] && [ "$actual" = minor ]; }; then
    problems+=("the commits need a $expected bump, but the version only has a $actual bump")
  fi
fi

if out=$(git ls-remote --tags origin "refs/tags/v$version" 2>/dev/null); then
  if [ -n "$out" ]; then
    problems+=("the tag v$version already exists on origin")
  else
    detail "- The tag \`v$version\` does not exist on origin yet."
  fi
else
  detail "- Could not read the tags on origin, so the check did not look for an existing \`v$version\` tag."
fi

if [ ${#problems[@]} -gt 0 ]; then
  fingerprint "$(printf '%s\n' "${problems[@]}")"
  fail "$(join_by '; ' "${problems[@]}")"
fi
if [ "$(rank "$actual")" -gt "$(rank "$expected")" ]; then
  decide "Release $version as a $actual bump, when the commits ask only for a $expected bump?" "The bump is larger than the commits need."
fi
pass "$version in every version file; $actual bump matches the commits"
