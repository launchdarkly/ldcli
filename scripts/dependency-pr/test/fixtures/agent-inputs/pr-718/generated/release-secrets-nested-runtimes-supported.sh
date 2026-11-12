#!/usr/bin/env bash
# Network: reads action.yml files through gh.
source "$VERIFY_ROOT/lib/check.sh"
command -v gh >/dev/null 2>&1 || incomplete "gh is not installed"
python3 -c 'import yaml' 2>/dev/null || incomplete "python3 with PyYAML is required"
updates_for github-actions | jq -r '.name' | sort -u >"$ARTIFACTS/names"
[ -s "$ARTIFACTS/names" ] || skip "no GitHub Actions updates"

python3 - "$WT" "$ARTIFACTS/names" >"$ARTIFACTS/report.json" <<'PY'
import base64, glob, json, subprocess, sys, yaml
wt, names = sys.argv[1], [l.strip() for l in open(sys.argv[2]) if l.strip()]
UNSUPPORTED = {"node12", "node16"}

def action_yml(spec):
    name, ref = spec.split("@", 1)
    parts = name.split("/")
    repo, sub = "/".join(parts[:2]), "/".join(parts[2:])
    for fn in ("action.yml", "action.yaml"):
        path = f"{sub}/{fn}" if sub else fn
        r = subprocess.run(["gh", "api", f"repos/{repo}/contents/{path}?ref={ref}", "--jq", ".content"], capture_output=True, text=True)
        if r.returncode == 0 and r.stdout.strip():
            return yaml.safe_load(base64.b64decode(r.stdout.strip()))
    return None

usages = {}
for p in sorted(glob.glob(f"{wt}/.github/**/*.y*ml", recursive=True)):
    d = yaml.safe_load(open(p)) or {}
    jobs = list((d.get("jobs") or {}).values()) + [{"steps": (d.get("runs") or {}).get("steps")}] if isinstance(d, dict) else []
    for job in jobs:
        for st in (job or {}).get("steps") or []:
            u = st.get("uses", "") if isinstance(st, dict) else ""
            if u.split("@")[0] in names and "@" in u:
                usages.setdefault(u, []).append({"file": p[len(wt) + 1:], "with": sorted((st.get("with") or {}).keys())})

report = {"usages": [], "unavailable": []}
for spec, uses in sorted(usages.items()):
    a = action_yml(spec)
    if a is None:
        report["unavailable"].append(spec)
        continue
    runs = a.get("runs") or {}
    entry = {"spec": spec, "files": sorted({u["file"] for u in uses}), "using": runs.get("using"), "nested": [], "unknown_inputs": []}
    declared = set((a.get("inputs") or {}).keys())
    entry["unknown_inputs"] = sorted({k for u in uses for k in u["with"] if k not in declared})
    for st in runs.get("steps") or []:
        nu = st.get("uses", "") if isinstance(st, dict) else ""
        if not nu or nu.startswith("./") or nu.startswith("docker://"):
            continue
        na = action_yml(nu)
        if na is None:
            report["unavailable"].append(nu)
            continue
        entry["nested"].append({"uses": nu, "using": (na.get("runs") or {}).get("using")})
    entry["unsupported"] = ([spec] if entry["using"] in UNSUPPORTED else []) + [n["uses"] for n in entry["nested"] if n["using"] in UNSUPPORTED]
    report["usages"].append(entry)
print(json.dumps(report, indent=2))
PY
[ $? -eq 0 ] || incomplete "could not analyze the action metadata"
cat "$ARTIFACTS/report.json"
[ "$(jq '.unavailable | length' "$ARTIFACTS/report.json")" -eq 0 ] ||
  incomplete "could not read action.yml for: $(jq -r '.unavailable | join(", ")' "$ARTIFACTS/report.json")"
jq -r '.usages[] | "- `\(.spec)` in \(.files | join(", ")): \(.using); nested: \([.nested[] | "`\(.uses)` \(.using)"] | join(", "))"' "$ARTIFACTS/report.json" >>"$ARTIFACTS/details.md"
bad_inputs=$(jq -r '[.usages[] | select(.unknown_inputs | length > 0) | "\(.spec): \(.unknown_inputs | join(", "))"] | join("; ")' "$ARTIFACTS/report.json")
[ -z "$bad_inputs" ] || fail "workflows pass inputs that the action does not declare: $bad_inputs"
bad=$(jq -r '[.usages[].unsupported[]] | unique | join(", ")' "$ARTIFACTS/report.json")
[ -z "$bad" ] || fail "the updated action runs nested actions on a Node.js runtime that GitHub no longer supports: $bad"
pass "every use of $(paste -sd, "$ARTIFACTS/names") and its nested actions declares a supported runtime, and the inputs passed exist"
