#!/usr/bin/env bash
# Lists every open Dependabot PR, what each one changes, and what main already
# has, then shows which of them overlap with the PR you're verifying.
#
#   scripts/other-prs.sh <pr-number>
#
# Run it from inside the ldcli checkout. It needs gh, git, and jq, fetches into
# refs/verify/ (scripts/cleanup.sh removes them), and never pushes.
#
# Changes are read from the manifests, not the PR title: go.mod, package.json,
# the top-level entries in package-lock.json, `uses:` lines under .github/, and
# FROM lines in Dockerfiles. Nested package-lock.json entries aren't listed.

set -euo pipefail

pr="${1:-}"
if ! [[ "$pr" =~ ^[0-9]+$ ]]; then
  echo "usage: $0 <pr-number>" >&2
  exit 1
fi
for tool in gh git jq; do
  command -v "$tool" >/dev/null || { echo "$tool is required" >&2; exit 1; }
done

remote="${VERIFY_REMOTE:-origin}"
repo="$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
lock_cap=12

gh pr list --author app/dependabot --state open --limit 200 \
  --json number,title,files \
  --jq '.[] | "\(.number)\t\(.title)\t\([.files[].path] | join(" "))"' >"$tmp/open.tsv"
if ! awk -F'\t' -v n="$pr" '$1 == n { found = 1 } END { exit !found }' "$tmp/open.tsv"; then
  gh pr view "$pr" --json number,title,files \
    --jq '"\(.number)\t\(.title)\t\([.files[].path] | join(" "))"' >>"$tmp/open.tsv"
fi
# The PR being verified goes first.
{ awk -F'\t' -v n="$pr" '$1 == n' "$tmp/open.tsv"; awk -F'\t' -v n="$pr" '$1 != n' "$tmp/open.tsv"; } >"$tmp/prs.tsv"

refspecs=("+main:refs/verify/main")
while IFS=$'\t' read -r n _ _; do
  refspecs+=("+pull/$n/head:refs/verify/pr-$n")
done <"$tmp/prs.tsv"
git -C "$repo" fetch --quiet "$remote" "${refspecs[@]}"
main="refs/verify/main"

show() { git -C "$repo" show "$1:$2" 2>/dev/null || true; }

gomod_versions() {
  show "$1" go.mod | awk '
    /^go [0-9]/ { print "go", $2; next }
    /^toolchain / { print "toolchain", $2; next }
    /^require \(/ { inblock = 1; next }
    inblock && /^\)/ { inblock = 0; next }
    inblock && NF >= 2 && $1 !~ /^\/\// { print $1, $2; next }
    /^require [^(]/ { print $2, $3 }'
}

npm_direct() {
  show "$1" "$2" | jq -r '((.dependencies // {}) + (.devDependencies // {})) | to_entries[] | "\(.key) \(.value)"' 2>/dev/null || true
}

npm_lock_top() {
  local lock="package-lock.json"
  [ "$2" != "." ] && lock="$2/package-lock.json"
  show "$1" "$lock" | jq -r '.packages // {} | to_entries[]
    | select(.key | test("^node_modules/(@[^/]+/)?[^/]+$"))
    | "\(.key | sub("^node_modules/"; "")) \(.value.version)"' 2>/dev/null || true
}

# A SHA-pinned action's version is the "# vX.Y.Z" comment after it.
uses_versions() {
  { git -C "$repo" grep -h -E '^[[:space:]]*-?[[:space:]]*uses:' "$1" -- .github 2>/dev/null || true; } |
    sed -E 's/^[[:space:]]*-?[[:space:]]*uses:[[:space:]]*//' |
    awk '{ split($1, a, "@"); v = a[2]; if ($2 == "#" && $3 != "") v = $3; if (a[2] != "") print a[1], v }' |
    sort -u
}

docker_from() {
  local f
  for f in $(git -C "$repo" ls-tree -r --name-only "$1" | grep -E '(^|/)Dockerfile[^/]*$' || true); do
    show "$1" "$f" | awk 'toupper($1) == "FROM" { n = split($2, a, ":"); print a[1], (n > 1 ? a[2] : "latest") }'
  done | sort -u
}

# Several versions of one name (an action used at v4 and v5) collapse to "v4,v5".
collapse() {
  sort -u | awk '{ if ($1 in v) v[$1] = v[$1] "," $2; else v[$1] = $2 } END { for (k in v) print k, v[k] }' | sort
}

diff_maps() {
  awk 'NR == FNR { o[$1] = $2; next } { n[$1] = $2 }
    END {
      for (k in n) if (!(k in o)) print k, "-", n[k]; else if (o[k] != n[k]) print k, o[k], n[k]
      for (k in o) if (!(k in n)) print k, o[k], "-"
    }' "$1" "$2" | sort
}

norm() { tr ',' '\n' | sed -E 's/^[~^>=v]+//; s/\+incompatible$//'; }
vmin() { printf '%s\n' "$1" | norm | sort -V | head -n1; }
vmax() { printf '%s\n' "$1" | norm | sort -V | tail -n1; }
ver_ge() { [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$2" ]; }

lookup() { awk -v k="$1" '$1 == k { print $2; exit }' "$2"; }

on_main_status() {
  local main_v="$1" new="$2"
  if [ "$new" = "-" ]; then
    [ -z "$main_v" ] && echo "removed on main"
    return 0
  fi
  [ -z "$main_v" ] && return 0
  if ver_ge "$(vmin "$main_v")" "$(vmax "$new")"; then echo "main has this or newer"; fi
}

gomod_versions "$main" | collapse >"$tmp/main.go"
uses_versions "$main" | collapse >"$tmp/main.uses"
docker_from "$main" | collapse >"$tmp/main.docker"

main_npm() {
  local key="${1//\//_}"
  if [ ! -f "$tmp/main.npm.$key" ]; then
    npm_lock_top "$main" "$1" | collapse >"$tmp/main.npm.$key"
  fi
  echo "$tmp/main.npm.$key"
}

# Writes "kind name old new main-version status" lines to $tmp/changes.<n>.
collect() {
  local n="$1" files="$2" head="refs/verify/pr-$1" base f dir mfile
  base="$(git -C "$repo" merge-base "$main" "$head")"
  : >"$tmp/changes.$n"

  emit() {
    local kind="$1" mfile="$2" name old new main_v
    while read -r name old new; do
      [ -n "$name" ] || continue
      main_v="$(lookup "$name" "$mfile")"
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$kind" "$name" "$old" "$new" "${main_v:--}" \
        "$(on_main_status "$main_v" "$new")" >>"$tmp/changes.$n"
    done
  }

  if [[ " $files " == *" go.mod "* ]]; then
    gomod_versions "$base" | collapse >"$tmp/old"
    gomod_versions "$head" | collapse >"$tmp/new"
    diff_maps "$tmp/old" "$tmp/new" | emit go "$tmp/main.go"
  fi

  local direct_names="$tmp/direct.$n"
  : >"$direct_names"
  for f in $files; do
    case "$f" in
      package.json | */package.json)
        dir="$(dirname "$f")"
        npm_direct "$base" "$f" | collapse >"$tmp/old"
        npm_direct "$head" "$f" | collapse >"$tmp/new"
        diff_maps "$tmp/old" "$tmp/new" | tee -a "$direct_names" | emit npm "$(main_npm "$dir")"
        ;;
    esac
  done
  for f in $files; do
    case "$f" in
      package-lock.json | */package-lock.json)
        dir="$(dirname "$f")"
        npm_lock_top "$base" "$dir" | collapse >"$tmp/old"
        npm_lock_top "$head" "$dir" | collapse >"$tmp/new"
        diff_maps "$tmp/old" "$tmp/new" |
          awk 'NR == FNR { d[$1] = 1; next } !($1 in d)' "$direct_names" - |
          emit lock "$(main_npm "$dir")"
        ;;
    esac
  done

  if [[ " $files " == *" .github/"* ]]; then
    uses_versions "$base" | collapse >"$tmp/old"
    uses_versions "$head" | collapse >"$tmp/new"
    diff_maps "$tmp/old" "$tmp/new" | emit action "$tmp/main.uses"
  fi
  if [[ " $files " =~ (^|[[:space:]/])Dockerfile ]]; then
    docker_from "$base" | collapse >"$tmp/old"
    docker_from "$head" | collapse >"$tmp/new"
    diff_maps "$tmp/old" "$tmp/new" | emit docker "$tmp/main.docker"
  fi
}

print_pr() {
  local n="$1" title="$2" label="$3" head="refs/verify/pr-$1" behind total on_main lock_total
  behind="$(git -C "$repo" rev-list --count "$head..$main")"
  total="$(wc -l <"$tmp/changes.$n")"
  on_main="$(awk -F'\t' '$6 != ""' "$tmp/changes.$n" | wc -l)"
  local state=""
  if [ "$total" -gt 0 ] && [ "$on_main" -eq "$total" ]; then
    state="  SUPERSEDED: main already has every change"
  elif [ "$on_main" -gt 0 ]; then
    state="  main already has $on_main of $total changes"
  fi
  echo "#$n$label  $behind behind main$state"
  echo "  title: $title"
  awk -F'\t' '$1 != "lock"' "$tmp/changes.$n" |
    awk -F'\t' '{ printf "  %-7s %-44s %s -> %s   main: %s%s\n", $1, $2, $3, $4, $5, ($6 != "" ? "   (" $6 ")" : "") }'
  lock_total="$(awk -F'\t' '$1 == "lock"' "$tmp/changes.$n" | wc -l)"
  awk -F'\t' '$1 == "lock"' "$tmp/changes.$n" | head -n "$lock_cap" |
    awk -F'\t' '{ printf "  %-7s %-44s %s -> %s   main: %s%s\n", $1, $2, $3, $4, $5, ($6 != "" ? "   (" $6 ")" : "") }'
  if [ "$lock_total" -gt "$lock_cap" ]; then
    echo "  ...and $((lock_total - lock_cap)) more package-lock.json changes"
  fi
  [ "$total" -eq 0 ] && echo "  (no version changes found in the files this script reads)"
  echo
}

while IFS=$'\t' read -r n _ files; do
  collect "$n" "$files"
done <"$tmp/prs.tsv"

count="$(wc -l <"$tmp/prs.tsv")"
echo "Open Dependabot PRs: $((count - 1)) besides #$pr. main is at $(git -C "$repo" rev-parse --short "$main")."
echo

first=1
while IFS=$'\t' read -r n title _; do
  if [ "$first" = 1 ]; then
    print_pr "$n" "$title" " (the PR you're verifying)"
    first=0
    echo "== Other open Dependabot PRs"
    echo
  else
    print_pr "$n" "$title" ""
  fi
done <"$tmp/prs.tsv"

echo "== Overlap with #$pr"
target_files="$(awk -F'\t' -v n="$pr" '$1 == n { print $3 }' "$tmp/prs.tsv")"
overlaps=0
while IFS=$'\t' read -r n title files; do
  [ "$n" = "$pr" ] && continue
  shared_files="$(comm -12 <(tr ' ' '\n' <<<"$target_files" | sort -u) <(tr ' ' '\n' <<<"$files" | sort -u) | tr '\n' ' ')"
  # Direct dependencies first, then packages that only appear in the lockfiles.
  shared="$(awk -F'\t' 'NR == FNR { v[$2] = $4; k[$2] = $1; next }
      ($2 in v) { print ((k[$2] == "lock" && $1 == "lock") ? 1 : 0) "\t" $2 "\t" v[$2] "\t" $4 }' \
      "$tmp/changes.$pr" "$tmp/changes.$n" | sort -t $'\t' -k1,1n -k2,2 | cut -f2-)"
  [ -z "$shared_files" ] && [ -z "$shared" ] && continue
  overlaps=$((overlaps + 1))
  echo "#$n  $title"
  [ -n "$shared_files" ] && echo "  both change: $shared_files"
  if [ -n "$shared" ]; then
    shown=0
    while IFS=$'\t' read -r name ours theirs; do
      if [ "$shown" -ge 8 ]; then
        echo "  ...and $(( $(wc -l <<<"$shared") - 8 )) more shared packages"
        break
      fi
      if [ "$ours" = "$theirs" ]; then rel="same version"
      elif [ "$ours" = "-" ] || [ "$theirs" = "-" ]; then rel="one of them removes it"
      elif ver_ge "$(vmax "$ours")" "$(vmax "$theirs")"; then rel="#$pr goes further"
      else rel="#$n goes further"
      fi
      echo "  $name: #$pr -> $ours, #$n -> $theirs ($rel)"
      shown=$((shown + 1))
    done <<<"$shared"
  fi
done <"$tmp/prs.tsv"
[ "$overlaps" -eq 0 ] && echo "No other open Dependabot PR changes the same packages or files."
exit 0
