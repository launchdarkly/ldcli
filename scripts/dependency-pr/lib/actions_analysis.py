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


def on_github_com(value):
    """A default as github.com evaluates it. Actions write GHES fallbacks as
    `github.server_url == 'https://github.com' && X || Y`, which is X there."""
    if not isinstance(value, str):
        return value
    m = re.fullmatch(r"\$\{\{\s*(.*?)\s*\}\}", value.strip(), re.S)
    if not m:
        return value.strip()
    expr = m.group(1)
    ghes = re.fullmatch(r"github\.server_url\s*==\s*'https://github\.com'\s*&&\s*(.+?)\s*\|\|\s*.+", expr, re.S)
    return "${{ " + (ghes.group(1) if ghes else expr).strip() + " }}"


def step_label(step):
    """A short name for a step: its name, the action (the owner for <owner>/action), or its command."""
    if step.get("name"):
        return str(step["name"])
    uses = str(step.get("uses") or "").split("@")[0]
    if uses:
        parts = uses.split("/")
        return parts[0] if len(parts) > 1 and parts[1] == "action" else uses
    run = str(step.get("run") or "").strip().split()
    return f"`{run[0]}`" if run else "the next step"


# The Python on the PATH of GitHub-hosted runner images.
RUNNER_PYTHON = {"ubuntu-latest": "3.12", "ubuntu-24.04": "3.12", "ubuntu-22.04": "3.10"}


def default_effects(name, change, usages, wt):
    """What a changed input default does to the steps after the action, for
    defaults whose effect is known. An empty list means: name the default."""
    if name != "actions/setup-python" or change["input"] != "python-version" or change["to"] not in (None, ""):
        return []
    pinned = os.path.join(wt, ".python-version")
    if os.path.isfile(pinned):
        with open(pinned) as f:
            target = f"the Python in .python-version ({f.read().strip()})"
    else:
        versions = {RUNNER_PYTHON.get(r) for us in usages for r in us["runners"]}
        target = f"the runner's Python {versions.pop()}" if len(versions) == 1 and None not in versions else "the runner's Python"
    old = f"the newest {change['from']} in the tool cache" if str(change["from"]).endswith(".x") else f"Python {change['from']}"
    out = []
    for us in usages:
        who = us["next_step"] or "the job"
        text = f"{who} in {os.path.basename(us['file'])} will use {target} instead of {old}"
        if text not in out:
            out.append(text)
    return out


EXACT = re.compile(r"^v?\d+\.\d+\.\d+$")
FLOATING = re.compile(r"^v?\d+(\.\d+)?$")


def vtuple(tag):
    return tuple(int(x) for x in re.findall(r"\d+", tag))


def gh_api(path):
    try:
        out = subprocess.run(["gh", "api", path], capture_output=True, text=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if out.returncode != 0:
        return None
    try:
        return json.loads(out.stdout)
    except ValueError:
        return None


_resolved = {}


def resolve_ref(name, ref):
    """The exact release that a ref points to today. A floating tag (v4, v4.1)
    moves with each release, so it resolves to the highest exact tag (v4.4.0)
    on the same commit. version is None when it cannot be resolved."""
    repo = "/".join(name.split("/")[:2])
    if EXACT.match(ref):
        return {"ref": ref, "floating": False, "version": ref}
    if not FLOATING.match(ref):
        return {"ref": ref, "floating": None, "version": None}
    if (repo, ref) in _resolved:
        return _resolved[(repo, ref)]
    commit = gh_api(f"repos/{repo}/commits/{ref}") or {}
    sha = commit.get("sha") if isinstance(commit, dict) else None
    refs = gh_api(f"repos/{repo}/git/matching-refs/tags/{ref}.") or []
    tags = sorted(((vtuple(t), t, r["object"]) for r in refs if isinstance(r, dict)
                   for t in [r.get("ref", "").removeprefix("refs/tags/")] if EXACT.match(t)), reverse=True)
    out = {"ref": ref, "floating": True, "version": None, "latest": tags[0][1] if tags else None}
    for _, tag, obj in tags[:20] if sha else []:
        target = obj.get("sha")
        if obj.get("type") == "tag":
            target = ((gh_api(f"repos/{repo}/git/tags/{target}") or {}).get("object") or {}).get("sha")
        if target == sha:
            out["version"] = tag
            break
    _resolved[(repo, ref)] = out
    return out


def stale_pins(name, old_refs, new_ref):
    """An exact new pin that is older than what a floating ref gives today:
    the floating major of the pin (@v6 is v6.1.0, the pin is v6.0.3), or a
    floating old ref that the PR replaces (@v4 is v4.4.0, the pin is v4.1.0)."""
    if not new_ref or not EXACT.match(new_ref):
        return []
    major = re.match(r"^v?\d+", new_ref).group(0)
    kinds = {r: "replaced" for r in old_refs}
    kinds.setdefault(major, "major")
    found = []
    for ref, kind in kinds.items():
        res = resolve_ref(name, ref)
        if res.get("floating") and res.get("version") and vtuple(res["version"]) > vtuple(new_ref):
            found.append({"pin": new_ref, "floating": ref, "resolves_to": res["version"], "kind": kind})
    return found


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
                steps = list(steps_of(job))
                for i, step in enumerate(steps):
                    if step.get("uses", "").split("@")[0] != name:
                        continue
                    wf_list = [f] if cdir is None else callers.get(cdir, [])
                    outputs = sorted(set(re.findall(r"steps\.%s\.outputs\.([\w-]+)" % re.escape(step["id"]), job_text))) if step.get("id") else []
                    usages.append({
                        "file": f, "job": job_id, "with": sorted((step.get("with") or {}).keys()),
                        "outputs_read": outputs, "workflows": wf_list,
                        "runs_on_pr": any(on_pr.get(w, False) for w in wf_list),
                        "runners": sorted({str(r) for w in wf_list for r in runners.get(w, set())}),
                        "next_step": step_label(steps[i + 1]) if i + 1 < len(steps) else None,
                    })
        entry = {"name": name, "from": u.get("from"), "to": u.get("to"), "breaking": bool(u.get("breaking")), "usages": usages,
                 "replaced": [r["version"] for r in u.get("replaced") or []]}

        # Every old ref that the PR replaces is compared with the new ref (a repo
        # can pin v4 in one workflow and v5 in another).
        old_refs = [r["ref"] for r in u.get("replaced") or []] or ([u["from_ref"]] if u.get("from_ref") else [])
        new_ref = u.get("to_ref")

        def label(ref, version):
            res = resolve_ref(name, ref)
            if res.get("floating") and res.get("version"):
                return f"{version} ({res['version']})"
            return version
        olds_shown = [label(r["ref"], r["version"]) for r in u.get("replaced") or []] or \
            ([label(u["from_ref"], u.get("from") or u["from_ref"])] if u.get("from_ref") else [u.get("from") or "∅"])
        new_shown = label(new_ref, u.get("to") or new_ref) if new_ref else (u.get("to") or "∅")
        entry["change"] = f"{', '.join(olds_shown)} → {new_shown}"
        entry["stale_pins"] = stale_pins(name, old_refs, new_ref)
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
                defaults_changed, defaults_same, new_required, outputs_missing = [], [], set(), set()
                for old in olds:
                    old_in, old_out = old.get("inputs") or {}, old.get("outputs") or {}
                    new_required |= {k for k, v in new_in.items()
                                     if k not in old_in and isinstance(v, dict) and v.get("required")
                                     and "default" not in v and k not in provided}
                    for k, v in sorted(new_in.items()):
                        change = {"input": k, "from": default(old_in.get(k)), "to": default(v)}
                        if k not in old_in or k in provided or str(change["from"]) == str(change["to"]):
                            continue
                        if str(on_github_com(change["from"])) == str(on_github_com(change["to"])):
                            if change not in defaults_same:
                                defaults_same.append(change)
                        elif change not in defaults_changed:
                            defaults_changed.append(change)
                    # An output that neither action.yml declares is set at run time
                    # (release-please-action), so only a declared output can go missing.
                    outputs_missing |= {o for o in read if o in old_out and o not in new_out}
                for c in defaults_changed:
                    c["effects"] = default_effects(name, c, usages, wt)
                old_runtimes = sorted({str((o.get("runs") or {}).get("using")) for o in olds})
                entry["upstream"] = {
                    "status": "ok",
                    "compared_refs": old_refs,
                    "inputs_used": sorted(provided),
                    "removed_but_used": sorted(k for k in provided
                                               if k not in new_in and any(k in (o.get("inputs") or {}) for o in olds)),
                    "new_required": sorted(new_required),
                    "defaults_changed": defaults_changed,
                    "defaults_same_on_github_com": defaults_same,
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
