#!/usr/bin/env bash
# A grouped Dependabot update lists its updates in the PR description. The
# commit can change more direct dependencies than the list names: #779 named
# dompurify and uuid, but also moved react-router, vite, and
# @launchpad-ui/components to new major versions. A reviewer who reads only the
# description does not see those updates.
source "$VERIFY_ROOT/lib/check.sh"

if [ "$(jq -r '.title == null and .body == null' "$PR_META")" = true ]; then
  skip "No PR description to compare (no PR metadata)"
fi
read -r security ndirect < <(jq -r '"\(.security) \(.direct_update_count)"' "$CLASSIFICATION")
grouped=false
jq -r '.title // ""' "$PR_META" | grep -qiE '\bgroup\b' && grouped=true
if [ "$grouped" != true ] && [ "$ndirect" -le 1 ]; then
  skip "Not a grouped update, and only one direct update"
fi
kind="update"
[ "$grouped" = true ] && kind="grouped update"
[ "$security" = true ] && kind="security $kind"

# Only Dependabot's own summary counts as disclosure. Bot blocks such as the
# Cursor Bugbot summary (<!-- CURSOR_SUMMARY --> … <!-- /CURSOR_SUMMARY -->)
# describe the whole diff, and the <details> sections quote upstream notes.
jq -r '(.title // "") + "\n" + (.body // "")' "$PR_META" |
  perl -0pe 's/<!--\s*([A-Za-z0-9_-]+)\s*-->.*?<!--\s*\/\1\s*-->//gs; s/<details\b.*?<\/details>//gis' \
    >"$ARTIFACTS/description.txt"

# A name counts as disclosed if the description has it as a whole token. For
# an action in a subdirectory (owner/repo/path), owner/repo also counts.
jq --rawfile text "$ARTIFACTS/description.txt" '
  def esc: gsub("(?<c>[.*+?^${}()|\\[\\]\\\\])"; "\\\(.c)");
  def named: ([.name] + (if .ecosystem == "github-actions" then [.name | split("/")[0:2] | join("/")] else [] end))
    | any(.[]; . as $n | $text | test("(^|[^A-Za-z0-9@/_.-])" + ($n | esc) + "($|[^A-Za-z0-9/_-])"));
  {named: [.updates[] | select(named) | {name, from, to, ecosystem}],
   undisclosed: [.updates[] | select(.direct and (named | not)) | {name, from, to, semver, ecosystem}],
   total: ([.updates[] | select(.direct)] | length)}' "$CLASSIFICATION" >"$ARTIFACTS/disclosure.json"
total=$(jq '.total' "$ARTIFACTS/disclosure.json")
n=$(jq '.undisclosed | length' "$ARTIFACTS/disclosure.json")
[ "$n" -eq 0 ] && pass "The PR description names all $total direct updates of this $kind"

# A Go module that a named update requires at the new version or higher is
# forced by that update (go.uber.org/mock v0.6.0 raises golang.org/x/term in
# #621). A focused PR cannot leave it out, so it needs no decision.
echo '{}' >"$ARTIFACTS/forced.json"
jq '[.named[] | select(.ecosystem == "gomod" and .to != null)]' "$ARTIFACTS/disclosure.json" >"$ARTIFACTS/roots.json"
jq '[.undisclosed[] | select(.ecosystem == "gomod" and .to != null)]' "$ARTIFACTS/disclosure.json" >"$ARTIFACTS/candidates.json"
if [ "$(jq 'length' "$ARTIFACTS/roots.json")" -gt 0 ] && [ "$(jq 'length' "$ARTIFACTS/candidates.json")" -gt 0 ]; then
  python3 "$VERIFY_ROOT/lib/deps.py" forced "$PR_WT" "$ARTIFACTS/roots.json" "$ARTIFACTS/candidates.json" \
    >"$ARTIFACTS/forced.json" 2>"$ARTIFACTS/forced.err" ||
    { cat "$ARTIFACTS/forced.err"; echo '{}' >"$ARTIFACTS/forced.json"; }
fi
jq -r --slurpfile f "$ARTIFACTS/forced.json" '.undisclosed[] | select($f[0][.name])
  | ($f[0][.name]) as $x
  | "\(.name) \(.from // "∅") → \(.to // "∅"), required by \($x.by)\(if ($x.via | length) > 0 then " through " + ($x.via | join(", ")) else "" end)"' \
  "$ARTIFACTS/disclosure.json" >"$ARTIFACTS/forced.txt"
jq -r --slurpfile f "$ARTIFACTS/forced.json" '.undisclosed[] | select($f[0][.name] | not)
  | "\(.name) \(.from // "∅") → \(.to // "∅") (\(.semver))"' "$ARTIFACTS/disclosure.json" >"$ARTIFACTS/undisclosed"

[ -s "$ARTIFACTS/forced.txt" ] && sed 's/^/- Not named, but forced: /' "$ARTIFACTS/forced.txt" >>"$ARTIFACTS/details.md"
if [ -s "$ARTIFACTS/undisclosed" ]; then
  sed 's/^/- Not named: /' "$ARTIFACTS/undisclosed" >>"$ARTIFACTS/details.md"
  m=$(wc -l <"$ARTIFACTS/undisclosed")
  list=$(paste -sd';' "$ARTIFACTS/undisclosed" | sed 's/;/; /g')
  named="$((total - n)) of them"
  [ "$n" -eq "$total" ] && named="none of them"
  findings_file "$ARTIFACTS/undisclosed"
  recommend "If these updates are not wanted, close the PR and update the named packages in a focused PR."
  decide "Accept the $m direct update(s) that the PR description does not name: $list?" \
    "This $kind changes $total direct dependencies. Dependabot's description names $named."
fi
info "The description does not name $n of $total direct update(s), but the updates that it names require them: $(paste -sd';' "$ARTIFACTS/forced.txt" | sed 's/;/; /g')"
