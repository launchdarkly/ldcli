#!/usr/bin/env python3
"""Reads and validates the agent's plan for a release run.

Usage: plan.py validate --out-dir DIR    (prints JSON; exit 1 only if a file is malformed)

Usage: plan.py checks --out-dir DIR      (prints the merged check manifest)

Files, relative to the out dir:
  inventory.json          from inventory.py
  agent/plan.json         units, claims, and findings (written by the agent)
  agent/units/<id>.json   optional: claims, findings, and checks of one unit, so
                          that parallel subagents do not edit the same file
  generated/checks.json   optional: the checks for the claims

Problems that more agent work can fix go to "problems". They make the verdict
incomplete. Malformed files go to "errors".
"""

import argparse
import json
import os
import re
import sys

RISK_ORDER = {"low": 0, "medium": 1, "high": 2}
ID_RE = re.compile(r"^[a-z0-9][a-z0-9-]*$")
KINDS = ("discriminating", "guard")
SEVERITIES = ("info", "decide", "block")


def load_json(path):
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


def short(sha):
    return (sha or "")[:7]


UNIT_FIELDS = ("claims", "findings", "not_verified", "risk_reasons")


def load(out_dir):
    """Returns (plan, manifest) with the unit fragments merged in.

    A fragment agent/units/<id>.json can hold claims, findings, not_verified,
    risk_reasons, a higher risk, and checks (the unit field is optional there).
    Raises json.JSONDecodeError or ValueError for a malformed file.
    """
    plan = load_json(os.path.join(out_dir, "agent", "plan.json"))
    manifest = load_json(os.path.join(out_dir, "generated", "checks.json"))
    if manifest is None:
        manifest = {"schema": 1, "checks": []}
    if plan is None:
        return None, manifest
    units = {u.get("id"): u for u in plan.get("units", []) if isinstance(u, dict)}
    frag_dir = os.path.join(out_dir, "agent", "units")
    if os.path.isdir(frag_dir):
        for name in sorted(os.listdir(frag_dir)):
            if not name.endswith(".json"):
                continue
            uid = name[:-5]
            frag = load_json(os.path.join(frag_dir, name))
            if uid not in units:
                raise ValueError(f"agent/units/{name}: the plan has no unit {uid}")
            u = units[uid]
            for k in UNIT_FIELDS:
                if k in frag:
                    u[k] = (u.get(k) or []) + frag[k] if isinstance(frag[k], list) else frag[k]
            if frag.get("risk") in RISK_ORDER and RISK_ORDER[frag["risk"]] > RISK_ORDER.get(u.get("risk"), -1):
                u["risk"] = frag["risk"]
            for c in frag.get("checks", []):
                c.setdefault("unit", uid)
                manifest.setdefault("checks", []).append(c)
    return plan, manifest


def validate(out_dir):
    errors, problems = [], []
    inv = load_json(os.path.join(out_dir, "inventory.json"))
    if inv is None:
        return {"errors": ["inventory.json is missing; run --phase prepare"], "problems": [], "present": False}
    try:
        plan, manifest = load(out_dir)
    except (json.JSONDecodeError, ValueError) as e:
        return {"errors": [f"invalid plan: {e}"], "problems": [], "present": True}
    if plan is None:
        return {"errors": [], "problems": [], "present": False}

    if plan.get("schema") != 1 or not isinstance(plan.get("units"), list):
        errors.append('agent/plan.json: expected {"schema": 1, "units": [...]}')
        return {"errors": errors, "problems": problems, "present": True}
    if manifest is not None and (manifest.get("schema") != 1 or not isinstance(manifest.get("checks"), list)):
        errors.append('generated/checks.json: expected {"schema": 1, "checks": [...]}')
        return {"errors": errors, "problems": problems, "present": True}
    checks = (manifest or {}).get("checks", [])

    registry = load_json(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "checks", "registry.json"))
    baseline_ids = {c["id"] for c in (registry or {}).get("checks", [])}
    commits = {c["short"]: c for c in inv["commits"] if not c.get("release_commit")}
    owner = {}
    units = {}
    for u in plan["units"]:
        uid = u.get("id", "")
        if not ID_RE.match(uid):
            errors.append(f"unit id {uid!r} must be kebab-case")
            continue
        if uid in units:
            errors.append(f"unit {uid} appears more than once")
            continue
        units[uid] = u
        for sha in u.get("commits", []):
            s = short(sha)
            if s not in commits:
                problems.append({"unit": uid, "text": f"Unit {uid} names commit {s}, which is not in the release range."})
                continue
            if s in owner:
                problems.append({"unit": uid, "text": f"Commit {s} is in two units: {owner[s]} and {uid}."})
            owner.setdefault(s, uid)
        floor = max((commits[short(s)]["risk"] for s in u.get("commits", []) if short(s) in commits),
                    key=RISK_ORDER.get, default="low")
        risk = u.get("risk")
        if risk not in RISK_ORDER:
            problems.append({"unit": uid, "text": f"Unit {uid} has no risk (low, medium, or high)."})
        elif RISK_ORDER[risk] < RISK_ORDER[floor]:
            problems.append({"unit": uid, "text": f"Unit {uid} has risk {risk}, but its commits are {floor} risk. You can raise a risk, but not lower it."})
        claims = u.get("claims", [])
        if not claims:
            problems.append({"unit": uid, "text": f"Unit {uid} has no claims. Write at least one claim for what the unit changes."})
        seen = set()
        for cl in claims:
            cid = cl.get("id", "")
            if not ID_RE.match(cid) or cid in seen:
                errors.append(f"unit {uid}: claim id {cid!r} must be kebab-case and unique")
            seen.add(cid)
            if not cl.get("text"):
                problems.append({"unit": uid, "text": f"Claim {uid}/{cid} has no text."})
            if not cl.get("checks") and not cl.get("baseline") and not cl.get("no_local_proof"):
                problems.append({"unit": uid, "text": f"Claim {uid}/{cid} has no check and no no_local_proof reason."})
            for bid in cl.get("baseline", []):
                if bid not in baseline_ids:
                    problems.append({"unit": uid, "text": f"Claim {uid}/{cid} names baseline check {bid}, which the registry does not have."})
        for i, f in enumerate(u.get("findings", [])):
            if f.get("severity") not in SEVERITIES:
                errors.append(f"unit {uid}: finding {i} severity must be one of {', '.join(SEVERITIES)}")
            if f.get("severity") == "decide" and not str(f.get("question", "")).rstrip().endswith("?"):
                problems.append({"unit": uid, "text": f"Finding {i} of unit {uid} is a decision, but it has no yes-or-no question."})

    for s in commits:
        if s not in owner:
            problems.append({"unit": None, "text": f"Commit {s} ({commits[s]['subject']}) is in no unit. Every commit in the release needs a unit."})

    check_ids = set()
    by_unit = {}
    for c in checks:
        cid = c.get("id", "")
        if not ID_RE.match(cid) or cid in check_ids:
            errors.append(f"check id {cid!r} must be kebab-case and unique")
            continue
        check_ids.add(cid)
        uid = c.get("unit")
        if uid not in units:
            problems.append({"unit": uid, "text": f"Check {cid} names unit {uid!r}, which the plan does not have."})
            continue
        by_unit.setdefault(uid, []).append(c)
        if c.get("kind") not in KINDS:
            errors.append(f"check {cid}: kind must be discriminating or guard")
        script = os.path.join(out_dir, "generated", c.get("script", ""))
        if not c.get("script") or not os.path.isfile(script):
            errors.append(f"check {cid}: script generated/{c.get('script', '')} not found")
        claim_ids = {cl.get("id") for cl in units[uid].get("claims", [])}
        for cl in c.get("claims", []):
            if cl not in claim_ids:
                problems.append({"unit": uid, "text": f"Check {cid} names claim {cl!r}, which unit {uid} does not have."})
        if not c.get("claims"):
            problems.append({"unit": uid, "text": f"Check {cid} names no claim."})
        for sha in c.get("revert", []):
            if short(sha) not in commits:
                problems.append({"unit": uid, "text": f"Check {cid} reverts {short(sha)}, which is not in the release range."})

    for uid, u in units.items():
        for cl in u.get("claims", []):
            for cid in cl.get("checks", []):
                if cid not in check_ids:
                    problems.append({"unit": uid, "text": f"Claim {uid}/{cl.get('id')} names check {cid}, which generated/checks.json does not have."})
        if u.get("risk") == "high" and not any(c.get("adversarial") for c in by_unit.get(uid, [])):
            problems.append({"unit": uid, "text": f"Unit {uid} is high risk, but it has no adversarial check (a check that tries to break the change)."})

    return {"errors": errors, "problems": problems, "present": True,
            "units": len(units), "checks": len(check_ids), "commits_covered": len(owner),
            "commits_total": len(commits)}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("command", choices=["validate", "checks"])
    ap.add_argument("--out-dir", required=True)
    a = ap.parse_args()
    if a.command == "checks":
        _, manifest = load(a.out_dir)
        print(json.dumps(manifest, indent=2))
        return
    res = validate(a.out_dir)
    print(json.dumps(res, indent=2))
    sys.exit(1 if res["errors"] else 0)


if __name__ == "__main__":
    main()
