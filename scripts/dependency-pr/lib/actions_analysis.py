#!/usr/bin/env python3
"""Where each bumped action is used, whether those workflows run on pull_request,
and (when gh is available) whether the inputs the repo passes still exist upstream.

Usage: actions_analysis.py <worktree> <updates.json>   (prints a JSON report)
"""
import base64
import glob
import json
import os
import subprocess
import sys

import yaml


def load(path):
    with open(path) as f:
        return yaml.safe_load(f) or {}


def triggers(doc):
    # PyYAML (YAML 1.1) parses the bare key `on` as boolean True.
    on = doc.get("on", doc.get(True))
    if isinstance(on, str):
        return [on]
    if isinstance(on, list):
        return on
    if isinstance(on, dict):
        return list(on.keys())
    return []


def steps_of(doc):
    for job_id, job in (doc.get("jobs") or {}).items():
        if not isinstance(job, dict):
            continue
        if isinstance(job.get("uses"), str):
            yield job_id, {"uses": job["uses"], "with": job.get("with") or {}}
        for step in job.get("steps") or []:
            if isinstance(step, dict):
                yield job_id, step
    runs = doc.get("runs") or {}
    for step in runs.get("steps") or []:
        if isinstance(step, dict):
            yield "(composite)", step


def gh_action_yml(name, ref):
    parts = name.split("/")
    repo, sub = "/".join(parts[:2]), "/".join(parts[2:])
    for fname in ("action.yml", "action.yaml"):
        path = f"{sub}/{fname}" if sub else fname
        try:
            out = subprocess.run(
                ["gh", "api", f"repos/{repo}/contents/{path}?ref={ref}", "--jq", ".content"],
                capture_output=True, text=True, timeout=60,
            )
        except (OSError, subprocess.TimeoutExpired):
            return None
        if out.returncode == 0 and out.stdout.strip():
            return yaml.safe_load(base64.b64decode(out.stdout.strip()))
    return None


def main():
    wt, updates_path = sys.argv[1], sys.argv[2]
    with open(updates_path) as f:
        updates = json.load(f)

    workflows = {}
    for p in sorted(glob.glob(os.path.join(wt, ".github/workflows/*.y*ml"))):
        workflows[os.path.relpath(p, wt)] = load(p)
    composites = {}
    for p in sorted(glob.glob(os.path.join(wt, ".github/actions/**/action.y*ml"), recursive=True)):
        composites[os.path.relpath(os.path.dirname(p), wt)] = (os.path.relpath(p, wt), load(p))

    on_pr = {f: bool({"pull_request", "pull_request_target", "merge_group"} & set(map(str, triggers(d))))
             for f, d in workflows.items()}
    callers = {}
    for f, d in workflows.items():
        for _, step in steps_of(d):
            uses = step.get("uses", "")
            if uses.startswith("./"):
                callers.setdefault(uses[2:].rstrip("/"), []).append(f)

    report = []
    for u in updates:
        name = u["name"]
        usages = []
        sources = [(f, d, None) for f, d in workflows.items()]
        sources += [(path, d, cdir) for cdir, (path, d) in composites.items()]
        for f, d, cdir in sources:
            for job, step in steps_of(d):
                uses = step.get("uses", "")
                if uses.split("@")[0] != name:
                    continue
                if cdir is None:
                    wfs = [f]
                else:
                    wfs = callers.get(cdir, [])
                usages.append({
                    "file": f,
                    "job": job,
                    "with": sorted((step.get("with") or {}).keys()),
                    "workflows": wfs,
                    "runs_on_pr": any(on_pr.get(w, False) for w in wfs),
                })
        entry = {"name": name, "from": u.get("from"), "to": u.get("to"), "usages": usages}

        if u.get("breaking") and u.get("from_refs") and u.get("to_refs"):
            old = gh_action_yml(name, u["from_refs"][-1])
            new = gh_action_yml(name, u["to_refs"][-1])
            if old is None or new is None:
                entry["inputs"] = {"status": "unavailable"}
            else:
                old_in = old.get("inputs") or {}
                new_in = new.get("inputs") or {}
                used = sorted({k for us in usages for k in us["with"]})
                provided = {k for us in usages for k in us["with"]}
                entry["inputs"] = {
                    "status": "ok",
                    "used": used,
                    "removed_but_used": [k for k in used if k not in new_in],
                    "new_required": [k for k, v in new_in.items()
                                     if k not in old_in and isinstance(v, dict) and v.get("required")
                                     and "default" not in v and k not in provided],
                    "runs_using": [str((old.get("runs") or {}).get("using")), str((new.get("runs") or {}).get("using"))],
                }
        report.append(entry)

    json.dump({"workflows_on_pr": on_pr, "actions": report}, sys.stdout, indent=2)


if __name__ == "__main__":
    main()
