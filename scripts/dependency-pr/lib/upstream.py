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


def go_repo(name, origin_url):
    # golang.org/x/<name> is served from go.googlesource.com and mirrored to github.com/golang/<name>.
    m = re.match(r"^golang\.org/x/([^/]+)", name)
    if m:
        return f"golang/{m.group(1)}"
    return github_repo(origin_url) or github_repo("https://" + name)


def resolve(update, wt):
    """Returns (repo, from_ref, to_ref, subdir). subdir is the package directory in a monorepo."""
    eco, name = update["ecosystem"], update["name"]
    frm, to = update.get("from"), update.get("to")
    if eco == "gomod":
        info = {}
        for side, v in (("from", frm), ("to", to)):
            out = sh(["go", "mod", "download", "-json", f"{name}@{v}"], cwd=wt) if v else None
            origin = (json.loads(out).get("Origin") or {}) if out else {}
            info[side] = origin
        repo = go_repo(name, info["to"].get("URL"))
        tag = lambda o, v: (o.get("Ref") or "").replace("refs/tags/", "") or v
        return repo, tag(info["from"], frm), tag(info["to"], to), None
    if eco in ("npm-ui", "npm-wrapper"):
        meta = {}
        for side, v in (("from", frm), ("to", to)):
            out = sh(["npm", "view", f"{name}@{v}", "repository.url", "repository.directory", "gitHead", "--json"]) if v else None
            try:
                meta[side] = json.loads(out) if out else {}
            except json.JSONDecodeError:
                meta[side] = {}
            if isinstance(meta[side], str):
                meta[side] = {"repository.url": meta[side]}
        repo = github_repo(meta["to"].get("repository.url"))
        subdir = (meta["to"].get("repository.directory") or "").strip("/") or None
        tag = lambda v: f"{name}@{v}" if subdir else f"v{v}"
        return repo, meta["from"].get("gitHead") or tag(frm), meta["to"].get("gitHead") or tag(to), subdir
    if eco == "github-actions":
        repo = "/".join(name.split("/")[:2])
        sub = "/".join(name.split("/")[2:]) or None
        return repo, update.get("from_ref") or frm, update.get("to_ref") or to, sub
    return None, None, None, None


def changelog_section(repo, path, frm, to, to_tag):
    data = gh_json(f"repos/{repo}/contents/{path}?ref={to_tag}") if to_tag else None
    if not data or "content" not in data:
        return ""
    text = base64.b64decode(data["content"]).decode("utf-8", "replace").splitlines()
    start = next((i for i, l in enumerate(text) if l.startswith("#") and to.lstrip("v") in l), None)
    end = next((i for i, l in enumerate(text) if start is not None and i > start and l.startswith("#") and frm.lstrip("v") in l), None)
    return "\n".join(text[start:end if end else start + 400]) if start is not None else ""


def release_notes(repo, frm, to, to_tag, name=None, subdir=None):
    # In a monorepo, the package CHANGELOG has the per-version notes. The GitHub
    # releases can only link to it (react-router) or tag other packages.
    if subdir:
        text = changelog_section(repo, f"{subdir}/CHANGELOG.md", frm, to, to_tag)
        if text:
            return f"{subdir}/CHANGELOG.md", text
    lo, hi = vtuple(frm), vtuple(to)
    want_prefix = split_tag(to_tag)[0] if to_tag and not re.fullmatch(r"[0-9a-f]{40}", to_tag) else None
    pkg_prefix = f"{name}@" if subdir and name else None
    notes = []
    for page in (1, 2, 3):
        releases = gh_json(f"repos/{repo}/releases?per_page=100&page={page}") or []
        for rel in releases:
            tag = rel.get("tag_name") or ""
            if pkg_prefix and not tag.startswith(pkg_prefix):
                continue
            prefix, version = split_tag(tag)
            v = vtuple(version)
            if not v or not lo or not hi or not (lo < v <= hi):
                continue
            if want_prefix is not None and prefix != want_prefix:
                continue
            notes.append((v, f"## {tag}\n\n{rel.get('body') or ''}\n"))
        if len(releases) < 100:
            break
    if notes:
        return "release notes", "\n".join(n for _, n in sorted(notes, reverse=True))
    for path in ("CHANGELOG.md", "CHANGES.md", "HISTORY.md"):
        text = changelog_section(repo, path, frm, to, to_tag)
        if text:
            return path, text
    return None, ""


def commit_log(cmp):
    """Commit subjects from a compare result, for repositories without notes (golang.org/x/*)."""
    commits = (cmp or {}).get("commits") or []
    if not commits:
        return ""
    lines = [f"- {c['sha'][:7]} {(c.get('commit') or {}).get('message', '').splitlines()[0]}" for c in commits]
    head = f"## Commits ({len(commits)} of {cmp.get('total_commits')})"
    return head + "\n\n" + "\n".join(reversed(lines)) + "\n"


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
        repo, from_ref, to_ref, subdir = resolve(u, wt)
        entry["repo"] = repo
        if repo:
            cmp = gh_json(f"repos/{repo}/compare/{from_ref}...{to_ref}")
            if cmp:
                entry["compare_url"] = cmp.get("html_url")
                entry["commits"] = cmp.get("total_commits")
                entry["files_changed"] = len(cmp.get("files") or [])
            npm_subdir = subdir if u["ecosystem"] in ("npm-ui", "npm-wrapper") else None
            source, text = release_notes(repo, u["from"], u["to"], to_ref, u["name"], npm_subdir)
            if not text and u["ecosystem"] == "gomod":
                text = commit_log(cmp)
                source = "commit messages" if text else None
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
