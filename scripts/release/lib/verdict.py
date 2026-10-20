#!/usr/bin/env python3
"""Builds result.json for a release run.

Usage: verdict.py --out-dir DIR   (prints result.json)

Verdicts, from the worst:
  block            the release must not ship as it is
  incomplete       a required check, the plan, or the judge review is missing or proved nothing
  needs-human      the evidence is complete, and a person must answer the questions
  safe-to-release  every check and review ran and passed
"""

import argparse
import datetime
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import plan as planlib  # noqa: E402

VERDICT_ORDER = ["safe-to-release", "needs-human", "incomplete", "block"]
EXIT = {"safe-to-release": 0, "needs-human": 1, "block": 1, "incomplete": 2}
COUNTS = ("proven", "proven-by-revert", "holds")


def load(path, default=None):
    try:
        with open(path) as f:
            return json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        return default


def read_lines(path):
    try:
        with open(path) as f:
            return [x.strip() for x in f if x.strip()]
    except FileNotFoundError:
        return []


class Items:
    def __init__(self):
        self.blocks, self.decisions, self.incomplete, self.notes = [], [], [], []

    def verdict(self):
        if self.blocks:
            return "block"
        if self.incomplete:
            return "incomplete"
        if self.decisions:
            return "needs-human"
        return "safe-to-release"

    def extend(self, other):
        self.blocks += other.blocks
        self.decisions += other.decisions
        self.incomplete += other.incomplete
        self.notes += other.notes


# ------------------------------------------------------------------ baseline

def pre_existing(pr, base):
    if not base or base.get("status") != pr.get("status"):
        return False
    if pr.get("findings") and base.get("findings") is not None:
        return set(pr["findings"]) <= set(base["findings"])
    return bool(pr.get("fingerprint")) and pr.get("fingerprint") == base.get("fingerprint")


def baseline_outcome(entry, pr, base, items):
    cid, title = entry["id"], entry.get("title", entry["id"])
    required, gate = entry.get("required", False), entry.get("gate", False)
    src = f"baseline:{cid}"
    if pr is None:
        if required:
            items.incomplete.append({"source": src, "text": f"{title}: the check did not run."})
        return "not-run"
    st = pr.get("status")
    if st in ("pass", "info"):
        return st
    if st == "skip":
        if pr.get("profile_skipped") and required:
            items.incomplete.append({"source": src, "text": f"{title}: runs only with --profile full."})
            return "not-run"
        return "skip"
    if st == "fail":
        if pre_existing(pr, base):
            if gate:
                items.blocks.append({"source": src, "text": f"{title}: {pr.get('summary')}. The last release had the same failure, but this check must pass.",
                                     "fix": pr.get("recommendations") or []})
                return "fail-pre-existing-gate"
            items.notes.append({"source": src, "text": f"{title}: {pr.get('summary')} (the last release had the same failure)."})
            return "pre-existing"
        items.blocks.append({"source": src, "text": f"{title}: {pr.get('summary')}", "fix": pr.get("recommendations") or [],
                             "recipe": pr.get("fix")})
        return "fail"
    if st == "decide":
        if pre_existing(pr, base):
            items.notes.append({"source": src, "text": f"{title}: {pr.get('summary')} (same as in the last release)."})
            return "pre-existing"
        items.decisions.append({"source": src, "question": pr.get("question") or f"Accept: {title}?",
                                "evidence": pr.get("summary"), "fix": pr.get("recommendations") or []})
        return "decide"
    # incomplete or error
    if required:
        items.incomplete.append({"source": src, "text": f"{title}: {pr.get('summary') or st}"})
    else:
        items.notes.append({"source": src, "text": f"{title} did not run: {pr.get('summary') or st}"})
    return st or "error"


# ------------------------------------------------------------------ unit checks

def check_outcome(kind, base, pr, revert):
    b = (base or {}).get("status")
    p = (pr or {}).get("status")
    r = (revert or {}).get("status") if revert else None
    if p is None or b is None:
        return "not-run"
    p = "pass" if p == "info" else p
    b = "pass" if b == "info" else b
    r = "pass" if r == "info" else r
    if p not in ("pass", "fail"):
        return "incomplete"
    if b in ("error", "incomplete", "decide"):
        return "incomplete"
    absent = b in ("fail", "skip")
    if kind == "discriminating":
        if absent and p == "pass":
            if r is None:
                return "proven"
            if r == "fail" or r == "skip":
                return "proven-by-revert"
            if r == "pass":
                return "not-discriminating"
            return "proven"
        if b == "pass" and p == "pass":
            return "not-discriminating"
        if b == "pass" and p == "fail":
            return "regression"
        return "fails-both"
    # guard
    if p == "pass" and b in ("pass", "skip"):
        return "holds"
    if p == "fail" and b in ("pass", "skip"):
        return "regression"
    return "invalid"


def unit_checks(out_dir, manifest, judgement, items):
    state = os.path.join(out_dir, "state", "generated")
    judged = {j.get("id"): j for j in (judgement or {}).get("checks", [])}
    results = {}
    for c in manifest.get("checks", []):
        cid, uid = c["id"], c.get("unit")
        d = os.path.join(state, cid)
        base = load(os.path.join(d, "base", "result.json"))
        pr = load(os.path.join(d, "pr", "result.json"))
        revert = load(os.path.join(d, "revert", "result.json")) if c.get("revert") else None
        outcome = check_outcome(c.get("kind"), base, pr, revert)
        j = judged.get(cid)
        assessment = (j or {}).get("assessment")
        counts = outcome in COUNTS
        src = f"unit:{uid}:{cid}"
        title = c.get("title") or cid
        summary = (pr or {}).get("summary") or ""
        if judgement is not None and j is None and outcome != "not-run":
            items.incomplete.append({"source": src, "unit": uid, "text": f"The judge did not review check {cid}."})
        if outcome == "not-run":
            items.incomplete.append({"source": src, "unit": uid, "text": f"Check {cid} has not run. Run --phase units."})
        elif outcome in ("incomplete", "invalid"):
            why = summary or (base or {}).get("summary") or outcome
            items.incomplete.append({"source": src, "unit": uid, "text": f"Check {cid} is {outcome}: {why}"})
        elif outcome == "not-discriminating":
            side = "with the commits reverted" if revert and revert.get("status") == "pass" else "on the last release"
            items.notes.append({"source": src, "unit": uid, "text": f"Check {cid} also passes {side}, so it is not proof of the change."})
        elif outcome in ("regression", "fails-both"):
            if assessment in ("false-positive", "vacuous"):
                items.notes.append({"source": src, "unit": uid, "text": f"Check {cid} failed, but the judge found a fault in the check: {(j or {}).get('reason', '')}"})
            elif assessment in ("confirmed-bug", "sound") or (outcome == "regression" and assessment is None):
                items.blocks.append({"source": src, "unit": uid, "text": f"{title}: {summary}",
                                     "evidence": (j or {}).get("reason"), "fix": (pr or {}).get("recommendations") or []})
            elif assessment == "unclear":
                items.decisions.append({"source": src, "unit": uid, "question": f"Is this a bug in the release: {title} ({summary})?",
                                        "evidence": (j or {}).get("reason")})
            else:
                items.incomplete.append({"source": src, "unit": uid, "text": f"Check {cid} fails on the candidate and does not pass on the last release. The judge must confirm a bug, or the check needs a fix."})
        if assessment == "unsafe":
            counts = False
            items.incomplete.append({"source": src, "unit": uid, "text": f"The judge found check {cid} unsafe: {(j or {}).get('reason', '')}. Rewrite it."})
        elif counts and judgement is not None and assessment not in ("sound", None):
            counts = False
            items.notes.append({"source": src, "unit": uid, "text": f"The judge does not accept check {cid} as proof ({assessment}): {(j or {}).get('reason', '')}"})
        if counts and judgement is not None and j is None:
            counts = False
        if revert and revert.get("revert_conflict") and outcome == "proven":
            items.notes.append({"source": src, "unit": uid, "text": f"Check {cid}: the revert conflicts, so the proof compares with the last release only."})
        results[cid] = {"id": cid, "unit": uid, "title": title, "kind": c.get("kind"),
                        "adversarial": bool(c.get("adversarial")), "claims": c.get("claims", []),
                        "rationale": c.get("rationale"), "outcome": outcome, "counts": counts,
                        "judge": j, "base": base, "pr": pr, "revert": revert}
    return results


# ------------------------------------------------------------------ main

def build(out_dir):
    meta = load(os.path.join(out_dir, "state", "pr.json"), {})
    inv = load(os.path.join(out_dir, "inventory.json"), {})
    try:
        plan, manifest = planlib.load(out_dir)
    except (json.JSONDecodeError, ValueError):
        plan, manifest = None, {"schema": 1, "checks": []}
    judgement = load(os.path.join(out_dir, "agent", "judgement.json"))
    validation = planlib.validate(out_dir)

    overall = Items()

    # Baseline
    baseline = []
    for cid in read_lines(os.path.join(out_dir, "state", "check-order.txt")):
        d = os.path.join(out_dir, "state", "checks", cid)
        entry = load(os.path.join(d, "entry.json"), {"id": cid})
        pr, base = load(os.path.join(d, "pr", "result.json")), load(os.path.join(d, "base", "result.json"))
        outcome = baseline_outcome(entry, pr, base, overall)
        baseline.append({"id": cid, "title": entry.get("title"), "required": entry.get("required", False),
                         "gate": entry.get("gate", False), "outcome": outcome, "pr": pr, "base": base})
    if not baseline:
        overall.incomplete.append({"source": "baseline", "text": "The baseline checks did not run. Run --phase baseline."})
    baseline_by_id = {b["id"]: b for b in baseline}

    # Plan
    for e in validation.get("errors", []):
        overall.incomplete.append({"source": "plan", "text": e})
    if not validation.get("present"):
        overall.incomplete.append({"source": "plan", "text": "No agent plan. Follow the verify-release skill to write agent/plan.json and the unit checks."})
    elif judgement is None:
        overall.incomplete.append({"source": "judge", "text": "No judge review. A separate agent must review the evidence and write agent/judgement.json."})

    unit_items = {}
    for p in validation.get("problems", []):
        unit_items.setdefault(p.get("unit"), Items()).incomplete.append({"source": "plan", "unit": p.get("unit"), "text": p["text"]})

    check_items = Items()
    checks = unit_checks(out_dir, manifest, judgement, check_items) if plan else {}
    for kind in ("blocks", "decisions", "incomplete", "notes"):
        for it in getattr(check_items, kind):
            getattr(unit_items.setdefault(it.get("unit"), Items()), kind).append(it)

    for g in (judgement or {}).get("gaps", []):
        unit_items.setdefault(g.get("unit"), Items()).incomplete.append(
            {"source": "judge:gap", "unit": g.get("unit"), "text": f"The judge found a change that no claim covers: {g.get('text')}"})

    judged_findings = {(f.get("unit"), f.get("index")): f for f in (judgement or {}).get("findings", [])}
    judged_claims = {(c.get("unit"), c.get("claim")): c for c in (judgement or {}).get("claims", [])}
    commits = {c["short"]: c for c in inv.get("commits", [])}
    units = []
    for u in (plan or {}).get("units", []):
        uid = u.get("id")
        it = unit_items.setdefault(uid, Items())
        claims = []
        for cl in u.get("claims", []):
            covered_by = [cid for cid, r in checks.items() if r["unit"] == uid and cl.get("id") in r["claims"] and r["counts"]]
            covered_by += [f"baseline:{bid}" for bid in cl.get("baseline", [])
                           if baseline_by_id.get(bid, {}).get("outcome") in ("pass", "info")]
            jc = judged_claims.get((uid, cl.get("id")))
            status = "covered" if covered_by else "not-covered"
            if covered_by and jc and jc.get("assessment") == "not-covered":
                status = "not-covered"
                it.notes.append({"source": f"judge:{uid}", "unit": uid, "text": f"The judge says the checks do not cover claim {cl.get('id')}: {jc.get('reason', '')}"})
            if status == "not-covered":
                if cl.get("no_local_proof"):
                    status = "no-local-proof"
                    it.decisions.append({"source": f"unit:{uid}", "unit": uid,
                                         "question": f"Release without a local check of this claim: {cl.get('text')}?",
                                         "evidence": cl.get("no_local_proof")})
                else:
                    it.incomplete.append({"source": f"unit:{uid}", "unit": uid,
                                          "text": f"Claim {uid}/{cl.get('id')} has no check that counts as proof: {cl.get('text')}"})
            claims.append({"id": cl.get("id"), "text": cl.get("text"), "source": cl.get("source"),
                           "status": status, "covered_by": covered_by, "no_local_proof": cl.get("no_local_proof")})
        for i, f in enumerate(u.get("findings", [])):
            sev, jf = f.get("severity"), judged_findings.get((uid, i))
            a = (jf or {}).get("assessment")
            src = f"finding:{uid}:{i}"
            if sev == "info":
                it.notes.append({"source": src, "unit": uid, "text": f.get("text"), "evidence": f.get("evidence")})
            elif sev in ("block", "decide") and a == "rejected":
                it.notes.append({"source": src, "unit": uid, "text": f"The judge rejected this finding: {f.get('text')} ({jf.get('reason', '')})"})
            elif sev == "block" and a == "unclear":
                it.decisions.append({"source": src, "unit": uid, "question": f.get("question") or f"Is this a release blocker: {f.get('text')}?",
                                     "evidence": f.get("evidence")})
            elif sev == "block":
                it.blocks.append({"source": src, "unit": uid, "text": f.get("text"), "evidence": f.get("evidence"),
                                  "fix": [f["fix"]] if f.get("fix") else []})
                if judgement is not None and jf is None:
                    it.incomplete.append({"source": src, "unit": uid, "text": f"The judge did not review blocking finding {i} of unit {uid}."})
            elif sev == "decide":
                it.decisions.append({"source": src, "unit": uid, "question": f.get("question"), "evidence": f.get("evidence") or f.get("text")})
        units.append({
            "id": uid, "title": u.get("title"), "risk": u.get("risk"), "risk_reasons": u.get("risk_reasons", []),
            "commits": [{"short": s[:7], "subject": commits.get(s[:7], {}).get("subject"), "pr": commits.get(s[:7], {}).get("pr")}
                        for s in u.get("commits", [])],
            "claims": claims, "checks": [r for r in checks.values() if r["unit"] == uid],
            "not_verified": u.get("not_verified"),
            "verdict": it.verdict(),
            "blocks": it.blocks, "decisions": it.decisions, "incomplete": it.incomplete, "notes": it.notes,
        })
    for key, it in unit_items.items():
        if key is None or key not in {u["id"] for u in units}:
            overall.incomplete += it.incomplete
            overall.blocks += it.blocks
            overall.decisions += it.decisions
            overall.notes += it.notes
    for u in units:
        overall.blocks += u["blocks"]
        overall.decisions += u["decisions"]
        overall.incomplete += u["incomplete"]
        overall.notes += u["notes"]

    verdict = overall.verdict()
    return {
        "schema": 1,
        "generated_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "release": {k: meta.get(k) for k in ("repo", "number", "url", "title", "head_ref", "head_sha", "since",
                                              "base_sha", "parent_sha", "version", "base_version")},
        "verdict": verdict,
        "exit_code": EXIT[verdict],
        "blocks": overall.blocks, "decisions": overall.decisions,
        "incomplete": overall.incomplete, "notes": overall.notes,
        "baseline": baseline,
        "units": units,
        "plan": {"present": validation.get("present", False), "summary": (plan or {}).get("summary"),
                 "commits_covered": validation.get("commits_covered"), "commits_total": validation.get("commits_total")},
        "judge": {"present": judgement is not None, "judge": (judgement or {}).get("judge"),
                  "notes": (judgement or {}).get("notes")},
        "inventory": {"counts": inv.get("counts"), "flags": inv.get("flags", [])},
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out-dir", required=True)
    a = ap.parse_args()
    print(json.dumps(build(a.out_dir), indent=2))


if __name__ == "__main__":
    main()
