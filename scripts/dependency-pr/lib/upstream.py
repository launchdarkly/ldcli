#!/usr/bin/env python3
"""Collects upstream evidence for each direct update: the source repository,
the compare link and size between the two versions, and the release notes
(or CHANGELOG section) in that range. It flags notes that mention breaking
changes, security fixes, deprecations, new requirements, or licenses.

Usage: upstream.py <classification.json> <worktree> <notes-dir>   (prints JSON)
Needs: gh (authenticated), go and npm for resolving sources.
"""
import base64
import json
import os
import re
import subprocess
import sys

KEYWORDS = {
    "breaking": r"breaking|backwards?[- ]incompatible|\bmajor change|\bremoved?\b.*\b(api|support|option|method|function)|\brewrite\b",
    "security": r"security|vulnerab|\bcve-\d|\bghsa-",
    "deprecation": r"deprecat",
    "requirements": r"minimum (go|node|version)|requires? (go|node)|engines|drop(ped|s)? support|node ?\d\d",
    "license": r"\blicen[cs]e",
}
VERSION_RE = re.compile(r"(\d+(?:\.\d+)+(?:-[0-9A-Za-z.]+)?)(?!.*\d+\.\d+)")


def sh(cmd, cwd=None):
    out = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=120)
    return out.stdout if out.returncode == 0 else None


def gh_json(path):
    out = sh(["gh", "api", path])
    return json.loads(out) if out else None


def vtuple(v):
    core = re.search(r"\d+(?:\.\d+)*", v or "")
    if not core:
        return None
    nums = [int(n) for n in core.group(0).split(".")][:3]
    return tuple(nums + [0] * (3 - len(nums)))


def split_tag(tag):
    m = VERSION_RE.search(tag or "")
    if not m:
        return None, None
    return tag[: m.start()].rstrip("v"), m.group(1)


def github_repo(url):
    m = re.search(r"github\.com[/:]([^/]+)/([^/#?]+?)(?:\.git)?(?:[/#?]|$)", url or "")
    return f"{m.group(1)}/{m.group(2)}" if m else None


def resolve(update, wt):
    eco, name = update["ecosystem"], update["name"]
    frm, to = update.get("from"), update.get("to")
    if eco == "gomod":
        info = {}
        for side, v in (("from", frm), ("to", to)):
            out = sh(["go", "mod", "download", "-json", f"{name}@{v}"], cwd=wt) if v else None
            origin = (json.loads(out).get("Origin") or {}) if out else {}
            info[side] = origin
        repo = github_repo(info["to"].get("URL")) or github_repo("https://" + name)
        tag = lambda o, v: (o.get("Ref") or "").replace("refs/tags/", "") or v
        return repo, tag(info["from"], frm), tag(info["to"], to)
    if eco in ("npm-ui", "npm-wrapper"):
        meta = {}
        for side, v in (("from", frm), ("to", to)):
            out = sh(["npm", "view", f"{name}@{v}", "repository.url", "gitHead", "--json"]) if v else None
            try:
                meta[side] = json.loads(out) if out else {}
            except json.JSONDecodeError:
                meta[side] = {}
            if isinstance(meta[side], str):
                meta[side] = {"repository.url": meta[side]}
        repo = github_repo(meta["to"].get("repository.url"))
        return repo, meta["from"].get("gitHead") or f"v{frm}", meta["to"].get("gitHead") or f"v{to}"
    if eco == "github-actions":
        repo = "/".join(name.split("/")[:2])
        return repo, (update.get("from_refs") or [frm])[-1], (update.get("to_refs") or [to])[-1]
    return None, None, None


def release_notes(repo, frm, to, to_tag):
    lo, hi = vtuple(frm), vtuple(to)
    want_prefix = split_tag(to_tag)[0] if to_tag and not re.fullmatch(r"[0-9a-f]{40}", to_tag) else None
    notes = []
    for page in (1, 2, 3):
        releases = gh_json(f"repos/{repo}/releases?per_page=100&page={page}") or []
        for rel in releases:
            prefix, version = split_tag(rel.get("tag_name"))
            v = vtuple(version)
            if not v or not lo or not hi or not (lo < v <= hi):
                continue
            if want_prefix is not None and prefix != want_prefix:
                continue
            notes.append((v, f"## {rel.get('tag_name')}\n\n{rel.get('body') or ''}\n"))
        if len(releases) < 100:
            break
    if notes:
        return "release notes", "\n".join(n for _, n in sorted(notes, reverse=True))
    for path in ("CHANGELOG.md", "CHANGES.md", "HISTORY.md"):
        data = gh_json(f"repos/{repo}/contents/{path}?ref={to_tag}") if to_tag else None
        if not data or "content" not in data:
            continue
        text = base64.b64decode(data["content"]).decode("utf-8", "replace").splitlines()
        start = next((i for i, l in enumerate(text) if l.startswith("#") and to.lstrip("v") in l), None)
        end = next((i for i, l in enumerate(text) if start is not None and i > start and l.startswith("#") and frm.lstrip("v") in l), None)
        if start is not None:
            return path, "\n".join(text[start:end if end else start + 400])
    return None, ""


def main():
    cls_path, wt, notes_dir = sys.argv[1:4]
    os.makedirs(notes_dir, exist_ok=True)
    with open(cls_path) as f:
        cls = json.load(f)
    report = []
    for u in cls["updates"]:
        if not u.get("direct") or u["ecosystem"] == "docker" or not u.get("from") or not u.get("to"):
            continue
        entry = {"name": u["name"], "ecosystem": u["ecosystem"], "from": u["from"], "to": u["to"]}
        repo, from_ref, to_ref = resolve(u, wt)
        entry["repo"] = repo
        if repo:
            cmp = gh_json(f"repos/{repo}/compare/{from_ref}...{to_ref}")
            if cmp:
                entry["compare_url"] = cmp.get("html_url")
                entry["commits"] = cmp.get("total_commits")
                entry["files_changed"] = len(cmp.get("files") or [])
            source, text = release_notes(repo, u["from"], u["to"], to_ref)
            entry["notes_source"] = source
            if text:
                fname = re.sub(r"[^A-Za-z0-9._-]", "_", u["name"]) + ".md"
                with open(os.path.join(notes_dir, fname), "w") as f:
                    f.write(text)
                entry["notes_file"] = fname
                low = text.lower()
                entry["keywords"] = {k: len(re.findall(p, low)) for k, p in KEYWORDS.items() if re.search(p, low)}
        report.append(entry)
    json.dump(report, sys.stdout, indent=2)


if __name__ == "__main__":
    main()
