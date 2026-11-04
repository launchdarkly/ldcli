#!/usr/bin/env bash
# Network: reads the release-please-action source at the pinned commit through gh.
source "$VERIFY_ROOT/lib/check.sh"
command -v gh >/dev/null 2>&1 || incomplete "gh is not installed"
python3 -c 'import yaml' 2>/dev/null || incomplete "python3 with PyYAML is required"

python3 - "$WT" >"$ARTIFACTS/usage.json" <<'PY' || incomplete "could not parse the workflows"
import glob, json, re, sys, yaml
wt = sys.argv[1]
out = []
for p in sorted(glob.glob(f"{wt}/.github/workflows/*.y*ml")):
    d = yaml.safe_load(open(p)) or {}
    for jid, job in (d.get("jobs") or {}).items():
        text = json.dumps(job)
        for st in job.get("steps") or []:
            u = st.get("uses", "")
            if u.split("@")[0] != "googleapis/release-please-action":
                continue
            sid = st.get("id")
            read = sorted(set(re.findall(r"steps\.%s\.outputs\.([\w-]+)" % re.escape(sid), text))) if sid else []
            out.append({"file": p[len(wt) + 1:], "job": jid, "id": sid, "ref": u.split("@", 1)[1], "outputs": read})
print(json.dumps(out))
PY
cat "$ARTIFACTS/usage.json"
[ "$(jq 'length' "$ARTIFACTS/usage.json")" -gt 0 ] || skip "no workflow uses googleapis/release-please-action"

missing=()
while IFS=$'\x1f' read -r ref outputs; do
  src="$ARTIFACTS/index-$ref.ts"
  if [ ! -s "$src" ]; then
    gh api "repos/googleapis/release-please-action/contents/src/index.ts?ref=$ref" --jq .content 2>"$ARTIFACTS/gh.err" | base64 -d >"$src" ||
      incomplete "could not read src/index.ts at $ref: $(tail -n1 "$ARTIFACTS/gh.err")"
  fi
  for o in $outputs; do
    if grep -qE "['\"\`]$o['\"\`]" "$src"; then
      detail "- \`$o\` is set by the action at \`${ref:0:12}\` (\`$(grep -nE "['\"\`]$o['\"\`]" "$src" | head -n1 | sed 's/^\([0-9]*\):[[:space:]]*/src\/index.ts:\1: /' | cut -c1-110)\`)"
    else
      missing+=("$o@${ref:0:12}")
    fi
  done
done < <(jq -r '.[] | select(.outputs | length > 0) | [.ref, (.outputs | join(" "))] | join("\u001f")' "$ARTIFACTS/usage.json" | sort -u)
[ ${#missing[@]} -eq 0 ] || fail "release-please-action no longer sets output(s) that the workflows read: ${missing[*]}"
pass "the pinned release-please-action source sets every output that the workflows read ($(jq -r '[.[].outputs[]] | unique | join(", ")' "$ARTIFACTS/usage.json"))"
