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
read -r group security ndirect < <(jq -r '"\(.group) \(.security) \(.direct_update_count)"' "$CLASSIFICATION")
if [ "$group" != true ] && [ "$ndirect" -le 1 ]; then
  skip "Not a grouped update"
fi
kind="grouped update"
[ "$security" = true ] && kind="grouped security update"

# A name counts as disclosed if the title or body has it as a whole token. For
# an action in a subdirectory (owner/repo/path), owner/repo also counts.
jq -r '(.title // "") + "\n" + (.body // "")' "$PR_META" >"$ARTIFACTS/description.txt"
jq -r --rawfile text "$ARTIFACTS/description.txt" '
  def esc: gsub("(?<c>[.*+?^${}()|\\[\\]\\\\])"; "\\\(.c)");
  .updates[] | select(.direct)
  | ([.name] + (if .ecosystem == "github-actions" then [.name | split("/")[0:2] | join("/")] else [] end)) as $names
  | select(any($names[]; . as $n | $text | test("(^|[^A-Za-z0-9@/_.-])" + ($n | esc) + "($|[^A-Za-z0-9/_-])")) | not)
  | "\(.name) \(.from // "∅") → \(.to // "∅") (\(.semver))"' "$CLASSIFICATION" >"$ARTIFACTS/undisclosed"

total=$(jq '[.updates[] | select(.direct)] | length' "$CLASSIFICATION")
if [ -s "$ARTIFACTS/undisclosed" ]; then
  sed 's/^/- /' "$ARTIFACTS/undisclosed" >>"$ARTIFACTS/details.md"
  n=$(wc -l <"$ARTIFACTS/undisclosed")
  list=$(paste -sd';' "$ARTIFACTS/undisclosed" | sed 's/;/; /g')
  findings_file "$ARTIFACTS/undisclosed"
  recommend "If these updates are not wanted, close the PR and update the disclosed packages in a focused PR."
  named="$((total - n)) of them"
  [ "$n" -eq "$total" ] && named="none of them"
  decide "Accept the $n direct update(s) that the PR description does not name: $list?" \
    "This $kind changes $total direct dependencies. The title and body name $named."
fi
pass "The PR description names all $total direct updates of this $kind"
