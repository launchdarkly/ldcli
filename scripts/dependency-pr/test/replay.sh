#!/usr/bin/env bash
# Replays past PR states through verify.sh and compares each result with the
# expectation in test/fixtures/<name>.json. The fixtures show that the verifier
# catches problems that past reviews missed or fixed by hand.
#
# Usage: scripts/dependency-pr/test/replay.sh [fixture-name...]
# Needs network (git fetch, module and npm downloads) and the tools that
# verify.sh needs. Output: .verify-out/replay/<name>/ and .verify-out/replay/summary.json
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(git -C "$ROOT" rev-parse --show-toplevel)"
FIXTURES="$ROOT/test/fixtures"
OUT="$REPO_ROOT/.verify-out/replay"
mkdir -p "$OUT"

names=("$@")
if [ ${#names[@]} -eq 0 ]; then
  for f in "$FIXTURES"/*.json; do names+=("$(basename "$f" .json)"); done
fi

failures=0
: >"$OUT/summary.jsonl"
for name in "${names[@]}"; do
  fx="$FIXTURES/$name.json"
  [ -f "$fx" ] || { echo "no fixture $fx"; failures=$((failures + 1)); continue; }
  dir="$OUT/$name"
  rm -rf "$dir/agent" "$dir/generated"
  mkdir -p "$dir"
  inputs=$(jq -r '.agent_inputs // empty' "$fx")
  if [ -n "$inputs" ]; then
    cp -r "$FIXTURES/$inputs/." "$dir/"
    chmod +x "$dir"/generated/*.sh 2>/dev/null || true
  fi
  envargs=()
  while IFS= read -r kv; do [ -n "$kv" ] && envargs+=("$kv"); done < <(jq -r '.env // {} | to_entries[] | "\(.key)=\(.value)"' "$fx")
  # A fixture can pin the PR title and body (pr_meta) for checks that read them.
  meta=(--no-pr-meta)
  pr_meta=$(jq -r '.pr_meta // empty' "$fx")
  [ -n "$pr_meta" ] && meta=(--pr-meta "$FIXTURES/$pr_meta")
  start=$(date +%s)
  env "${envargs[@]}" "$ROOT/verify.sh" --pr "$(jq -r .pr "$fx")" --head-sha "$(jq -r .head "$fx")" \
    --base-sha "$(jq -r .base "$fx")" "${meta[@]}" --profile "$(jq -r '.profile // "fast"' "$fx")" \
    --out-dir "$dir" >"$dir/verify.log" 2>&1
  code=$?
  secs=$(($(date +%s) - start))
  if [ ! -f "$dir/result.json" ]; then
    echo "FAIL $name: verify.sh exited $code without result.json (see $dir/verify.log)"
    failures=$((failures + 1))
    continue
  fi
  # Each expectation that does not hold becomes one line of "problems".
  problems=$(jq -r --slurpfile fx "$fx" '
    . as $r | $fx[0].expect as $e
    | ($r.checks | map({(.id): .outcome}) | add // {}) as $outcomes
    | [ (if $e.verdict and $r.verdict != $e.verdict then "verdict \($r.verdict), expected \($e.verdict)" else empty end),
        (if $e.verdict_not and $r.verdict == $e.verdict_not then "verdict must not be \($e.verdict_not)" else empty end),
        (($e.checks // {}) | to_entries[] | select($outcomes[.key] != .value)
          | "check \(.key): \($outcomes[.key] // "not run"), expected \(.value)"),
        (($e.summaries_contain // {}) | to_entries[] as $s
          | select(([$r.checks[] | select(.id == $s.key) | .pr.summary] | join("\n")) | contains($s.value) | not)
          | "check \($s.key) summary does not mention \"\($s.value)\""),
        (($e.blocks_contain // [])[] as $s | select(([$r.blocks[] | .problem + " " + (.fix | join(" "))] | join("\n")) | contains($s) | not)
          | "no block mentions \"\($s)\""),
        (($e.decisions_contain // [])[] as $s | select(([$r.decisions[].question] | join("\n")) | contains($s) | not)
          | "no decision mentions \"\($s)\""),
        (($e.incomplete_contain // [])[] as $s | select(([$r.incomplete[].reason] | join("\n")) | contains($s) | not)
          | "no incomplete item mentions \"\($s)\""),
        (($e.fixes // [])[] as $id | select([$r.fixes[].id] | index($id) | not) | "no fix recipe \($id)"),
        (($e.decisions_exclude // [])[] as $s | select(([$r.decisions[].question] | join("\n")) | contains($s))
          | "a decision mentions \"\($s)\""),
        (($e.updates // {}) | to_entries[] as $u
          | ([$r.classification.updates[] | select(.name == $u.key)] | first) as $got
          | select($got == null or ($u.value | to_entries | any(.value != $got[.key])))
          | "update \($u.key): \(if $got == null then "not found" else ($u.value | keys | map("\(.) \($got[.] | tostring)") | join(", ")) end), expected \($u.value | to_entries | map("\(.key) \(.value | tostring)") | join(", "))")
      ] | .[]' "$dir/result.json")
  verdict=$(jq -r .verdict "$dir/result.json")
  if [ -z "$problems" ]; then
    echo "ok   $name: $verdict (${secs}s)"
  else
    echo "FAIL $name: $verdict (${secs}s)"
    sed 's/^/     /' <<<"$problems"
    failures=$((failures + 1))
  fi
  jq -n --arg name "$name" --arg verdict "$verdict" --argjson secs "$secs" --arg problems "$problems" \
    '{name: $name, verdict: $verdict, seconds: $secs, ok: ($problems == ""), problems: ($problems | split("\n") | map(select(length > 0)))}' >>"$OUT/summary.jsonl"
done
jq -s '.' "$OUT/summary.jsonl" >"$OUT/summary.json"

echo
if [ "$failures" -gt 0 ]; then
  echo "$failures fixture(s) failed"
  exit 1
fi
echo "all fixtures passed"
