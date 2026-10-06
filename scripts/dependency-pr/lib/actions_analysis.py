#!/usr/bin/env python3
"""For each updated action: where the repo uses it, whether those workflows run
on pull_request, and (with gh) how the upstream action.yml changed between the
two refs: inputs, input defaults, outputs that the repo reads, and runtime.
It also compares the `permissions` blocks of all workflows between base and PR.

Usage: actions_analysis.py <pr-worktree> <updates.json> <base-worktree>   (prints JSON)
"""
import base64
import glob
import json
import os
import re
import subprocess
import sys

import yaml

GITHUB_HOSTED = re.compile(r"^(ubuntu|windows|macos)-[\w.-]+$")


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


def jobs_of(doc):
    for job_id, job in (doc.get("jobs") or {}).items():
        if isinstance(job, dict):
            yield job_id, job
    runs = doc.get("runs") or {}
    if runs.get("steps"):
        yield "(composite)", {"steps": runs["steps"]}


def steps_of(job):
    if isinstance(job.get("uses"), str):
        yield {"uses": job["uses"], "with": job.get("with") or {}}
    for step in job.get("steps") or []:
        if isinstance(step, dict):
            yield step


def workflows(wt):
    return {os.path.relpath(p, wt): load(p) for p in sorted(glob.glob(os.path.join(wt, ".github/workflows/*.y*ml")))}


def permissions(wt):
    out = {}
    for f, d in workflows(wt).items():
        out[f"{f} (workflow)"] = d.get("permissions")
        for job_id, job in jobs_of(d):
            out[f"{f} job {job_id}"] = job.get("permissions")
    return out


def gh_action_yml(name, ref):
    parts = name.split("/")
    repo, sub = "/".join(parts[:2]), "/".join(parts[2:])
    for fname in ("action.yml", "action.yaml"):
        path = f"{sub}/{fname}" if sub else fname
        try:
            out = subprocess.run(["gh", "api", f"repos/{repo}/contents/{path}?ref={ref}", "--jq", ".content"],
                                 capture_output=True, text=True, timeout=60)
        except (OSError, subprocess.TimeoutExpired):
            return None
        if out.returncode == 0 and out.stdout.strip():
            return yaml.safe_load(base64.b64decode(out.stdout.strip()))
    return None


def main():
    wt, updates_path, base_wt = sys.argv[1], sys.argv[2], sys.argv[3]
    with open(updates_path) as f:
        updates = json.load(f)

    wfs = workflows(wt)
    composites = {}
    for p in sorted(glob.glob(os.path.join(wt, ".github/actions/**/action.y*ml"), recursive=True)):
        composites[os.path.relpath(os.path.dirname(p), wt)] = (os.path.relpath(p, wt), load(p))
    on_pr = {f: bool({"pull_request", "pull_request_target", "merge_group"} & set(map(str, triggers(d)))) for f, d in wfs.items()}
    callers, runners = {}, {}
    for f, d in wfs.items():
        for _, job in jobs_of(d):
            ro = job.get("runs-on")
            runners.setdefault(f, set()).update(ro if isinstance(ro, list) else [ro] if ro else [])
            for step in steps_of(job):
                uses = step.get("uses", "")
                if uses.startswith("./"):
                    callers.setdefault(uses[2:].rstrip("/"), []).append(f)

    report = []
    for u in updates:
        name = u["name"]
        usages = []
        sources = [(f, d, None) for f, d in wfs.items()] + [(path, d, cdir) for cdir, (path, d) in composites.items()]
        for f, d, cdir in sources:
            for job_id, job in jobs_of(d):
                job_text = json.dumps(job)
                for step in steps_of(job):
                    if step.get("uses", "").split("@")[0] != name:
                        continue
                    wf_list = [f] if cdir is None else callers.get(cdir, [])
                    outputs = sorted(set(re.findall(r"steps\.%s\.outputs\.([\w-]+)" % re.escape(step["id"]), job_text))) if step.get("id") else []
                    usages.append({
                        "file": f, "job": job_id, "with": sorted((step.get("with") or {}).keys()),
                        "outputs_read": outputs, "workflows": wf_list,
                        "runs_on_pr": any(on_pr.get(w, False) for w in wf_list),
                        "runners": sorted({str(r) for w in wf_list for r in runners.get(w, set())}),
                    })
        entry = {"name": name, "from": u.get("from"), "to": u.get("to"), "breaking": bool(u.get("breaking")), "usages": usages,
                 "replaced": [r["version"] for r in u.get("replaced") or []]}

        # Every old ref that the PR replaces is compared with the new ref (a repo
        # can pin v4 in one workflow and v5 in another).
        old_refs = [r["ref"] for r in u.get("replaced") or []] or ([u["from_ref"]] if u.get("from_ref") else [])
        new_ref = u.get("to_ref")
        if old_refs and new_ref:
            olds = [gh_action_yml(name, r) for r in old_refs]
            new = gh_action_yml(name, new_ref)
            if new is None or any(o is None for o in olds):
                entry["upstream"] = {"status": "unavailable"}
            else:
                new_in, new_out = new.get("inputs") or {}, new.get("outputs") or {}
                provided = {k for us in usages for k in us["with"]}
                read = {o for us in usages for o in us["outputs_read"]}
                default = lambda spec: (spec or {}).get("default") if isinstance(spec, dict) else None
                defaults_changed, new_required, outputs_missing = [], set(), set()
                for old in olds:
                    old_in, old_out = old.get("inputs") or {}, old.get("outputs") or {}
                    new_required |= {k for k, v in new_in.items()
                                     if k not in old_in and isinstance(v, dict) and v.get("required")
                                     and "default" not in v and k not in provided}
                    for k, v in sorted(new_in.items()):
                        change = {"input": k, "from": default(old_in.get(k)), "to": default(v)}
                        if k in old_in and k not in provided and str(change["from"]) != str(change["to"]) \
                                and change not in defaults_changed:
                            defaults_changed.append(change)
                    # An output that neither action.yml declares is set at run time
                    # (release-please-action), so only a declared output can go missing.
                    outputs_missing |= {o for o in read if o in old_out and o not in new_out}
                old_runtimes = sorted({str((o.get("runs") or {}).get("using")) for o in olds})
                entry["upstream"] = {
                    "status": "ok",
                    "compared_refs": old_refs,
                    "inputs_used": sorted(provided),
                    "removed_but_used": sorted(k for k in provided
                                               if k not in new_in and any(k in (o.get("inputs") or {}) for o in olds)),
                    "new_required": sorted(new_required),
                    "defaults_changed": defaults_changed,
                    "outputs_read": sorted(read),
                    "outputs_undeclared": sorted(o for o in read if o not in new_out and all(o not in (x.get("outputs") or {}) for x in olds)),
                    "outputs_missing": sorted(outputs_missing),
                    "runs_using": [", ".join(old_runtimes), str((new.get("runs") or {}).get("using"))],
                }
        entry["self_hosted_runners"] = sorted({r for us in usages for r in us["runners"] if not GITHUB_HOSTED.match(r)})
        report.append(entry)

    pb, pp = permissions(base_wt), permissions(wt)
    perm_changes = [{"where": k, "from": pb.get(k), "to": pp.get(k)} for k in sorted(set(pb) | set(pp)) if pb.get(k) != pp.get(k)]
    json.dump({"workflows_on_pr": on_pr, "actions": report, "permissions_changes": perm_changes}, sys.stdout, indent=2, default=str)


if __name__ == "__main__":
    main()
