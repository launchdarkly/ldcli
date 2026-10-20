#!/usr/bin/env python3
"""Renders report.md from result.json.  Usage: render.py result.json"""

import json
import sys

MARKER = "<!-- ldcli-release-verify -->"
ICON = {"pass": "pass", "info": "pass", "skip": "skip", "fail": "FAIL", "decide": "decide",
        "incomplete": "incomplete", "error": "error"}


def cell(text, limit=160):
    text = " ".join(str(text or "").split()).replace("|", "\\|")
    return text if len(text) <= limit else text[: limit - 1] + "…"


def fix_lines(item):
    out = []
    for f in item.get("fix") or []:
        out.append(f"   Fix: {f}")
    if item.get("evidence"):
        ev = item["evidence"]
        ev = "; ".join(ev) if isinstance(ev, list) else ev
        out.append(f"   Evidence: {cell(ev, 300)}")
    return out


def side(r):
    if not r:
        return "not run"
    return f"{r.get('status')}: {cell(r.get('summary'), 120)}"


def render(res):
    rel = res["release"]
    L = [MARKER, f"## Release verification: ldcli {rel.get('version') or ''}", ""]
    L.append(f"Verdict: `{res['verdict']}`")
    L.append("")
    counts = res.get("inventory", {}).get("counts") or {}
    n_units = len(res["units"])
    proven = sum(1 for u in res["units"] for c in u["checks"] if c["counts"])
    n_checks = sum(len(u["checks"]) for u in res["units"])
    passed = sum(1 for b in res["baseline"] if b["outcome"] in ("pass", "info", "skip"))
    L.append(
        f"The candidate `{(rel.get('head_sha') or '')[:7]}` is compared with the last release `{rel.get('since')}` "
        f"(`{(rel.get('base_sha') or '')[:7]}`). The release has {counts.get('commits', '?')} commits in {n_units} units. "
        f"{passed} of {len(res['baseline'])} baseline checks passed. {proven} of {n_checks} agent checks count as proof."
    )
    if res["plan"].get("summary"):
        L += ["", res["plan"]["summary"]]
    L.append("")

    if res["blocks"]:
        L += ["### What blocks the release", ""]
        for i, b in enumerate(res["blocks"], 1):
            unit = f" (unit `{b['unit']}`)" if b.get("unit") else ""
            L.append(f"{i}. {cell(b['text'], 1500)}{unit}")
            L += fix_lines(b)
        L.append("")
    if res["decisions"]:
        L += ["### Questions for a person", "", "Answer each question with yes or no.", ""]
        for i, d in enumerate(res["decisions"], 1):
            unit = f" (unit `{d['unit']}`)" if d.get("unit") else ""
            L.append(f"{i}. {cell(d['question'], 1500)}{unit}")
            L += fix_lines(d)
        L.append("")
    if res["incomplete"]:
        L += ["### What is incomplete", "", "More agent work or a rerun closes these items, not a person.", ""]
        for it in res["incomplete"]:
            L.append(f"- {cell(it['text'], 400)}")
        L.append("")

    if res["units"]:
        L += ["### Units", "", "| Unit | Risk | Commits | Claims with proof | Verdict |", "|---|---|---|---|---|"]
        for u in res["units"]:
            covered = sum(1 for c in u["claims"] if c["status"] == "covered")
            L.append(f"| `{u['id']}` {cell(u['title'], 60)} | {u['risk']} | {len(u['commits'])} | "
                     f"{covered} of {len(u['claims'])} | `{u['verdict']}` |")
        L.append("")

    L += ["### Baseline checks", "", "| Check | Result | Summary |", "|---|---|---|"]
    for b in res["baseline"]:
        pr = b.get("pr") or {}
        L.append(f"| {cell(b['title'], 80)} | {b['outcome']} | {cell(pr.get('summary'), 200)} |")
    L.append("")

    flags = res.get("inventory", {}).get("flags") or []
    if flags:
        L += ["<details><summary>Inventory flags</summary>", ""]
        for f in flags:
            L.append(f"- `{f['kind']}`: {cell(f['text'], 400)}")
        L += ["", "</details>", ""]

    for u in res["units"]:
        L += [f"<details><summary>Unit <code>{u['id']}</code>: {cell(u['title'], 80)} ({u['verdict']})</summary>", ""]
        for r in u.get("risk_reasons") or []:
            L.append(f"Risk: {cell(r, 300)}")
        L.append("")
        L.append("Commits: " + ", ".join(f"`{c['short']}`" + (f" (#{c['pr']})" if c.get("pr") else "") for c in u["commits"]))
        L.append("")
        L.append("Claims:")
        L.append("")
        for c in u["claims"]:
            by = ", ".join(f"`{x}`" for x in c["covered_by"]) or "none"
            L.append(f"- {c['status']}: {cell(c['text'], 300)} (source: {cell(c.get('source'), 60)}; proof: {by})")
        L.append("")
        if u["checks"]:
            L += ["| Check | Kind | Outcome | Last release | Candidate | Reverted | Judge |", "|---|---|---|---|---|---|---|"]
            for c in u["checks"]:
                kind = c["kind"] + (", adversarial" if c["adversarial"] else "")
                j = c.get("judge") or {}
                L.append(f"| `{c['id']}` | {kind} | {c['outcome']} | {side(c['base'])} | {side(c['pr'])} | "
                         f"{side(c['revert']) if c.get('revert') else '-'} | {cell(j.get('assessment') or '-', 40)} |")
            L.append("")
        if u.get("not_verified"):
            L += [f"Not verified: {cell(u['not_verified'], 600)}", ""]
        for n in u["notes"]:
            L.append(f"- Note: {cell(n['text'], 400)}")
        L += ["", "</details>", ""]

    if res["notes"]:
        L += ["<details><summary>All notes</summary>", ""]
        for n in res["notes"]:
            L.append(f"- {cell(n['text'], 400)}")
        L += ["", "</details>", ""]

    judge = res.get("judge") or {}
    L.append("---")
    L.append(f"Head `{rel.get('head_sha')}`. Generated {res['generated_at']}. "
             f"Judge: {judge.get('judge') or 'none'}. The verifier does not approve, merge, or tag.")
    return "\n".join(L) + "\n"


if __name__ == "__main__":
    with open(sys.argv[1]) as f:
        sys.stdout.write(render(json.load(f)))
