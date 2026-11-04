#!/usr/bin/env python3
"""Compares the workflows that PR CI must run on the PR head with the workflow
runs that GitHub created for that head.

A workflow is expected when it triggers on pull_request or pull_request_target,
its branch filter matches the base branch, its activity types include a push to
the PR, and its path filters match the changed files. pull_request workflows
come from the merged tree, pull_request_target workflows from the base branch.

Usage: ci_runs.py <pr-worktree> <base-worktree> <pr.json> <classification.json>   (prints JSON)

pr.json fields read: head_sha, base_ref, ci (the status-check rollup), ci_head
(the commit that the rollup belongs to), ci_runs (workflow runs from
`actions/runs?head_sha=`, each with jobs_count and failed_jobs).

A failure whose failed steps read only files that the PR does not change
(a go test step on an npm-only PR) is "rerun": the fix is a rerun, not a
change to the PR. The report also gives the age of the oldest run and the
commits on the base branch since then that changed a workflow.
"""
import datetime
import glob
import json
import os
import re
import subprocess
import sys

import yaml

PR_EVENTS = ("pull_request", "pull_request_target")
PUSH_TYPES = {"opened", "synchronize", "reopened"}
OK = {"success", "neutral", "skipped"}
ROLLUP_FAIL = {"FAILURE", "TIMED_OUT", "STARTUP_FAILURE", "ERROR"}
ROLLUP_PENDING = {"QUEUED", "IN_PROGRESS", "PENDING", "WAITING", "REQUESTED", "EXPECTED", "CANCELLED", "ACTION_REQUIRED", "STALE", ""}


def glob_regex(pattern):
    """A GitHub filter pattern as a regex: * stops at /, ** does not, and
    **/ also matches no directory. ? and + apply to the character before them."""
    out, i = "", 0
    while i < len(pattern):
        c = pattern[i]
        if pattern.startswith("**/", i):
            out += "(?:.*/)?"
            i += 3
            continue
        if pattern.startswith("**", i):
            out += ".*"
            i += 2
            continue
        if c == "*":
            out += "[^/]*"
        elif c in "?+":
            out += c
        elif c == "[":
            j = pattern.find("]", i)
            if j < 0:
                out += re.escape(c)
            else:
                out += pattern[i:j + 1]
                i = j
        else:
            out += re.escape(c)
        i += 1
    return re.compile(out + r"\Z")


def matches(patterns, value):
    """The last pattern that matches wins. A pattern that starts with ! excludes."""
    hit = False
    for p in patterns or []:
        p = str(p)
        neg = p.startswith("!")
        if glob_regex(p[1:] if neg else p).match(value):
            hit = not neg
    return hit


def event_config(doc, event):
    on = doc.get("on", doc.get(True))
    if isinstance(on, str):
        return {} if on == event else None
    if isinstance(on, list):
        return {} if event in on else None
    if isinstance(on, dict) and event in on:
        return on[event] or {}
    return None


def expected_reason(cfg, base_ref, changed):
    """None when the workflow runs for this PR, else the reason it does not."""
    types = cfg.get("types")
    if types and not (PUSH_TYPES & set(map(str, types))):
        return "its activity types do not include a push to the PR"
    if "branches" in cfg and not matches(cfg["branches"], base_ref):
        return f"its branch filter does not match {base_ref}"
    if "branches-ignore" in cfg and matches(cfg["branches-ignore"], base_ref):
        return f"it ignores the base branch {base_ref}"
    if "paths" in cfg and not any(matches(cfg["paths"], f) for f in changed):
        return "no changed file matches its paths filter"
    if "paths-ignore" in cfg and changed and all(matches(cfg["paths-ignore"], f) for f in changed):
        return "every changed file matches its paths-ignore filter"
    return None


def load_workflows(wt):
    out = {}
    for p in sorted(glob.glob(os.path.join(wt, ".github/workflows/*.y*ml"))):
        try:
            with open(p) as f:
                out[os.path.relpath(p, wt)] = yaml.safe_load(f) or {}
        except yaml.YAMLError:
            out[os.path.relpath(p, wt)] = None
    return out


def expected_workflows(pr_wt, base_wt, base_ref, changed):
    expected, not_expected = [], []
    sides = {"pull_request": load_workflows(pr_wt), "pull_request_target": load_workflows(base_wt)}
    for event in PR_EVENTS:
        for path, doc in sides[event].items():
            if doc is None:
                continue
            cfg = event_config(doc, event)
            if cfg is None:
                continue
            entry = {"path": path, "name": doc.get("name") or path, "event": event}
            why = expected_reason(cfg if isinstance(cfg, dict) else {}, base_ref, changed)
            (not_expected if why else expected).append(entry | ({"reason": why} if why else {}))
    return expected, not_expected


def run_state(run):
    """(state, reason): state is ok, fail, or incomplete."""
    status, concl, jobs = run.get("status"), run.get("conclusion"), run.get("jobs_count")
    since = (run.get("created_at") or "")[:10]
    if status != "completed":
        return "incomplete", f"{status or 'not started'} since {since}"
    if concl in OK:
        return "ok", concl
    if concl == "startup_failure":
        return "incomplete", "failed at startup, before any job ran"
    if concl in ("failure", "timed_out") and jobs == 0:
        return "incomplete", f"ended `{concl}` with 0 jobs, so no job ran"
    if concl in ("failure", "timed_out"):
        return "fail", f"`{concl}` in {jobs} job(s)" if jobs is not None else f"`{concl}`"
    if concl == "action_required":
        return "incomplete", "waits for approval to run"
    return "incomplete", f"ended `{concl}`"


def latest(runs):
    return max(runs, key=lambda r: (r.get("created_at") or "", r.get("run_attempt") or 0, r.get("id") or 0))


# What a `run:` step reads, by the commands in it. A step that matches none of
# these, or that calls an action, can read anything, so it counts as related.
STEP_ECOSYSTEMS = (
    ("gomod", re.compile(r"\bgo\s+(test|build|vet|run|generate|mod|install|list)\b|golangci-lint|govulncheck|goreleaser")),
    ("npm", re.compile(r"\b(npm|npx|yarn|pnpm|node)\b")),
    ("docker", re.compile(r"\bdocker\b")),
)


def find_job(doc, name):
    """The job that GitHub names `name`. A matrix job adds " (<values>)"."""
    jobs = (doc or {}).get("jobs") or {}
    for want in (name, re.sub(r" \(.*\)$", "", name)):
        for key, job in jobs.items():
            if isinstance(job, dict) and want in (key, str(job.get("name") or key)):
                return job
    return None


def find_step(job, name):
    """The step that GitHub names `name`: its name, or "Run <action or first command line>"."""
    for step in job.get("steps") or []:
        if not isinstance(step, dict):
            continue
        if step.get("name") is not None:
            if str(step["name"]) == name:
                return step
            continue
        lines = str(step.get("run") or "").strip().splitlines()
        label = str(step.get("uses") or (lines[0] if lines else ""))
        if label and name == f"Run {label}":
            return step
    return None


def step_ecosystems(step, job):
    """The ecosystems whose files a failed step reads, or None when unknown."""
    if step.get("uses") or not step.get("run"):
        return None
    workdir = str(step.get("working-directory") or ((job.get("defaults") or {}).get("run") or {}).get("working-directory") or "")
    text = f"{workdir}\n{step['run']}"
    found = set()
    for eco, pattern in STEP_ECOSYSTEMS:
        if pattern.search(text):
            found.add(eco)
    if "npm" in found:
        found.discard("npm")
        found.add("npm-ui" if "internal/dev_server/ui" in text else "npm-wrapper")
    return found or None


def unrelated_failure(run, doc, wf_path, cls):
    """When every failed step of a failed run reads only files that the PR
    does not change, a text that names those steps. Such a failure (a flaky
    test on main) needs a rerun, not a fix in the PR. Else None."""
    failed = run.get("failed_jobs")
    if not failed or doc is None or wf_path in (cls.get("changed_files") or []):
        return None
    pr_ecos = set(cls.get("ecosystems") or [])
    updated_actions = {u["name"] for u in cls.get("updates") or [] if u.get("ecosystem") == "github-actions"}
    parts = []
    for fj in failed:
        job = find_job(doc, fj.get("name") or "")
        if job is None or not fj.get("steps"):
            return None
        # An updated action earlier in the job (setup-go) can change what a later step does.
        if any(isinstance(s, dict) and (str(s.get("uses") or "").split("@")[0] in updated_actions
                                        or str(s.get("uses") or "").startswith("./"))
               for s in job.get("steps") or []) and "github-actions" in pr_ecos:
            return None
        for name in fj["steps"]:
            step = find_step(job, name)
            ecos = step_ecosystems(step, job) if step else None
            if ecos is None or ecos & pr_ecos:
                return None
            cmd = str(step["run"]).strip().splitlines()[0]
            parts.append(f"job {fj['name']}, step {name} (`{cmd}`, {', '.join(sorted(ecos))})")
    return "; ".join(parts) if parts else None


def main():
    pr_wt, base_wt, meta_path, cls_path = sys.argv[1:5]
    with open(meta_path) as f:
        meta = json.load(f)
    with open(cls_path) as f:
        cls = json.load(f)
    head = meta.get("head_sha") or ""
    changed = cls.get("changed_files") or []
    expected, not_expected = expected_workflows(pr_wt, base_wt, meta.get("base_ref") or "main", changed)

    report = {"head": head, "expected": expected, "not_expected": not_expected,
              "workflows": [], "rollup": [], "other_heads": [], "runs_known": meta.get("ci_runs") is not None}

    runs = meta.get("ci_runs") or []
    # A result for another commit says nothing about this head.
    report["other_heads"] = sorted({r.get("head_sha", "")[:7] for r in runs if r.get("head_sha") != head})
    runs = [r for r in runs if r.get("head_sha") == head]
    docs = {"pull_request": load_workflows(pr_wt), "pull_request_target": load_workflows(base_wt)}
    for wf in expected:
        mine = [r for r in runs if r.get("event") == wf["event"]
                and ((r.get("path") or "").split("@")[0] == wf["path"] or (not r.get("path") and r.get("name") == wf["name"]))]
        if not report["runs_known"]:
            state, reason, run = "incomplete", "workflow runs could not be read", None
        elif not mine:
            state, reason, run = "incomplete", "no run on this head", None
        else:
            run = latest(mine)
            state, reason = run_state(run)
            if state == "fail":
                where = unrelated_failure(run, docs[wf["event"]].get(wf["path"]), wf["path"], cls)
                if where:
                    state, reason = "rerun", f"{reason}, only in {where}, which this PR does not change"
                    wf = wf | {"rerun_jobs": [j["name"] for j in run["failed_jobs"]]}
        report["workflows"].append(wf | {"state": state, "reason": reason,
                                         "run": None if run is None else {k: run.get(k) for k in ("id", "status", "conclusion", "jobs_count", "created_at", "html_url")}})

    rollup_head = meta.get("ci_head")
    stale_rollup = bool(rollup_head) and rollup_head != head
    rerun = {(w["name"], j) for w in report["workflows"] if w["state"] == "rerun" for j in w["rerun_jobs"]}
    for c in meta.get("ci") or []:
        st = (c.get("conclusion") or c.get("state") or c.get("status") or "").upper()
        if stale_rollup:
            state, reason = "incomplete", f"result is for {rollup_head[:7]}, not this head"
        elif st in ROLLUP_FAIL and (c.get("workflow") or "", c.get("name") or "") in rerun:
            state, reason = "rerun", f"{st.lower()} in a job that this PR does not change"
        elif st in ROLLUP_FAIL:
            state, reason = "fail", st.lower()
        elif st in ROLLUP_PENDING:
            state, reason = "incomplete", st.lower() or "pending"
        else:
            state, reason = "ok", st.lower()
        report["rollup"].append({"name": c.get("name"), "workflow": c.get("workflow") or "", "state": state, "reason": reason})
    report.update(staleness(base_wt, [w for w in report["workflows"] if w["run"]], [w["path"] for w in expected]))
    json.dump(report, sys.stdout, indent=2)


def staleness(base_wt, ran, paths):
    """The age of the oldest run, and the commits on the base branch after that
    run that changed an expected workflow or a local action. Those runs did
    not test the workflows that a merge would use."""
    dates = sorted(w["run"]["created_at"] for w in ran if w["run"].get("created_at"))
    if not dates:
        return {"oldest_run": None, "age_days": None, "workflow_changes_since": None}
    oldest = dates[0]
    try:
        age = (datetime.datetime.now(datetime.timezone.utc)
               - datetime.datetime.fromisoformat(oldest.replace("Z", "+00:00"))).days
    except ValueError:
        age = None
    out = subprocess.run(["git", "-C", base_wt, "log", "--first-parent", f"--since={oldest}", "--format=%h %s", "--",
                          *paths, ".github/actions"], capture_output=True, text=True)
    changes = [l for l in out.stdout.splitlines() if l.strip()] if out.returncode == 0 else None
    return {"oldest_run": oldest, "age_days": age, "workflow_changes_since": changes}


if __name__ == "__main__":
    main()
