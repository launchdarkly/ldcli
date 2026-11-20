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
    "breaking": r"breaking|backwards?[- ]incompatible|\bmajor change|\bremoved?\b.*\b(api|support|options?|methods?|functions?|packages?|components?|exports?|props?|inputs?|outputs?)\b|\brewrite\b",
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

        def tag(v):
            # Without gitHead, use the first tag form that exists: "<name>@1.2.3"
            # (react-router, @launchpad-ui/*), "v1.2.3" (vite), or "1.2.3".
            forms = ([f"{name}@{v}"] if subdir else []) + [f"v{v}", v]
            if repo:
                for t in forms:
                    if sh(["gh", "api", f"repos/{repo}/git/ref/tags/{t}", "--jq", ".ref"]):
                        return t
                return None
            return forms[0]
        return repo, meta["from"].get("gitHead") or tag(frm), meta["to"].get("gitHead") or tag(to), subdir
    if eco == "github-actions":
        repo = "/".join(name.split("/")[:2])
        sub = "/".join(name.split("/")[2:]) or None
        return repo, update.get("from_ref") or frm, update.get("to_ref") or to, sub
    return None, None, None, None


EXACT = re.compile(r"^v?\d+\.\d+\.\d+$")
FLOATING = re.compile(r"^v?\d+(\.\d+)?$")


def resolve_floating(repo, ref):
    """The exact release that a floating action tag (v7, v4.1) points to today,
    or None. Read as a version, v7 is 7.0.0, which drops v7.0.1 to v7.x."""
    if not ref or not FLOATING.match(ref):
        return ref
    sha = (gh_json(f"repos/{repo}/commits/{ref}") or {}).get("sha")
    refs = gh_json(f"repos/{repo}/git/matching-refs/tags/{ref}.") or []
    tags = sorted(((vtuple(t), t, r.get("object") or {}) for r in refs if isinstance(r, dict)
                   for t in [r.get("ref", "").removeprefix("refs/tags/")] if EXACT.match(t)), reverse=True)
    for _, tag, obj in tags[:20] if sha else []:
        target = obj.get("sha")
        if obj.get("type") == "tag":
            target = ((gh_json(f"repos/{repo}/git/tags/{target}") or {}).get("object") or {}).get("sha")
        if target == sha:
            return tag
    return None


HEAD_VERSION = re.compile(r"^#+\s.*?\bv?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?)")
MAX_CHANGELOG_LINES = 6000
MAX_RELEASE_PAGES = 10


def changelog_section(repo, path, frm, to, to_tag):
    """Returns (text, coverage). The section starts at the heading of `to` and
    ends at the first release heading at or below `frm`. coverage is
    {"status": "full" | "partial", "lowest": <lowest version in the text>}."""
    data = None
    # A monorepo can publish a version without a tag (@launchpad-ui/core
    # 0.59.17). The default branch has the same CHANGELOG with newer entries.
    for ref in ([to_tag] if to_tag else []) + [None]:
        data = gh_json(f"repos/{repo}/contents/{path}" + (f"?ref={ref}" if ref else ""))
        if data and "content" in data:
            break
    if not data or "content" not in data:
        return "", None
    text = base64.b64decode(data["content"]).decode("utf-8", "replace").splitlines()
    want = to.lstrip("v")
    start = next((i for i, l in enumerate(text) if HEAD_VERSION.match(l) and HEAD_VERSION.match(l).group(1) == want), None)
    if start is None:
        start = next((i for i, l in enumerate(text) if l.startswith("#") and want in l), None)
    if start is None:
        return "", None
    lo, lowest = vtuple(frm), want
    for i in range(start + 1, min(len(text), start + MAX_CHANGELOG_LINES)):
        m = HEAD_VERSION.match(text[i])
        if not m:
            continue
        if lo and vtuple(m.group(1)) <= lo:
            return "\n".join(text[start:i]), bump_only({"status": "full", "lowest": lowest}, text[start:i])
        lowest = m.group(1)
    end = min(len(text), start + MAX_CHANGELOG_LINES)
    return "\n".join(text[start:end]), bump_only({"status": "partial", "lowest": lowest}, text[start:end])


BUMP_ONLY = re.compile(r"version bump only|there were no code changes", re.I)


def bump_only(cov, lines):
    """A meta package CHANGELOG (typescript-eslint) says "version bump only"
    for releases whose changes are in the packages that it re-exports. Such
    an entry is not a note, so the coverage is partial."""
    entries, current = [], None
    for line in lines:
        if HEAD_VERSION.match(line):
            current = []
            entries.append(current)
        elif current is not None and line.strip():
            current.append(line)
    # An entry that says so and lists no change item. The entries also have
    # boilerplate lines ("See GitHub Releases ...").
    n = sum(1 for e in entries if any(BUMP_ONLY.search(l) for l in e)
            and not any(re.match(r"\s*([-*]\s|#)", l) for l in e))
    if n:
        cov = cov | {"status": "partial", "bump_only": n, "entries": len(entries)}
    return cov


def release_notes(repo, frm, to, to_tag, name=None, subdir=None):
    """Returns (source, text, coverage)."""
    # In a monorepo, the package CHANGELOG has the per-version notes. The GitHub
    # releases can only link to it (react-router) or tag other packages.
    if subdir:
        text, cov = changelog_section(repo, f"{subdir}/CHANGELOG.md", frm, to, to_tag)
        if text and (cov or {}).get("bump_only") and name:
            extra, dirs = reexported_notes(repo, name, frm, to, to_tag)
            if extra:
                text += "\n\n" + extra
                cov = cov | {"reexported": dirs}
                if all(d["status"] == "full" for d in dirs):
                    cov["status"] = "full"
        if text:
            return f"{subdir}/CHANGELOG.md", text, cov
    lo, hi = vtuple(frm), vtuple(to)
    want_prefix = split_tag(to_tag)[0] if to_tag and not re.fullmatch(r"[0-9a-f]{40}", to_tag) else None
    pkg_prefix = f"{name}@" if subdir and name else None
    notes, reached, exhausted = [], False, False
    for page in range(1, MAX_RELEASE_PAGES + 1):
        releases = gh_json(f"repos/{repo}/releases?per_page=100&page={page}") or []
        for rel in releases:
            tag = rel.get("tag_name") or ""
            if pkg_prefix and not tag.startswith(pkg_prefix):
                continue
            prefix, version = split_tag(tag)
            if want_prefix is not None and prefix != want_prefix:
                continue
            v = vtuple(version)
            if not v or not lo or not hi:
                continue
            if v <= lo:
                reached = True
            elif v <= hi:
                notes.append((v, version, f"## {tag}\n\n{rel.get('body') or ''}\n"))
        if len(releases) < 100:
            exhausted = True
            break
        if reached:
            break
    if notes:
        notes.sort(reverse=True)
        cov = {"status": "full" if reached or exhausted else "partial", "lowest": notes[-1][1]}
        return "release notes", "\n".join(n for _, _, n in notes), cov
    for path in ("CHANGELOG.md", "CHANGES.md", "HISTORY.md"):
        text, cov = changelog_section(repo, path, frm, to, to_tag)
        if text:
            return path, text, cov
    return None, "", None


def reexported_notes(repo, name, frm, to, to_tag):
    """The CHANGELOG sections of the packages that a meta package depends on
    at the same version and that live in the same repository."""
    out = sh(["npm", "view", f"{name}@{to}", "dependencies", "--json"])
    try:
        deps = json.loads(out) if out else {}
    except json.JSONDecodeError:
        deps = {}
    texts, dirs = [], []
    for dep, spec in sorted((deps or {}).items()):
        if str(spec).lstrip("^~=") != to:
            continue
        meta = sh(["npm", "view", f"{dep}@{to}", "repository", "--json"])
        try:
            meta = json.loads(meta) if meta else {}
        except json.JSONDecodeError:
            meta = {}
        d = (meta.get("directory") or "").strip("/") if isinstance(meta, dict) else ""
        if not d or github_repo(meta.get("url")) != repo:
            continue
        text, cov = changelog_section(repo, f"{d}/CHANGELOG.md", frm, to, to_tag)
        if text:
            texts.append(f"# {dep} (re-exported by {name})\n\n{text}")
            dirs.append({"package": dep, "status": (cov or {}).get("status", "none")})
    return "\n\n".join(texts), dirs


def missing_commits(repo, from_ref, to_ref, cmp):
    """When the new ref does not contain the old ref (GitHub reports
    "diverged"), the commits of the old ref that the new one lacks."""
    if (cmp or {}).get("status") != "diverged":
        return None
    back = gh_json(f"repos/{repo}/compare/{to_ref}...{from_ref}") or {}
    commits = back.get("commits") or []
    # A bare #N in an upstream subject would link to an ldcli PR.
    subject = lambda c: re.sub(r"(?<![\w/.#-])#(\d+)\b", rf"{repo}#\1", (c.get("commit") or {}).get("message", "").splitlines()[0])
    return {"count": cmp.get("behind_by") or len(commits),
            "commits": [f"{c['sha'][:7]} {subject(c)}" for c in commits[-20:]]}


def commit_log(cmp):
    """Commit subjects from a compare result, for repositories without notes (golang.org/x/*)."""
    commits = (cmp or {}).get("commits") or []
    if not commits:
        return "", None
    lines = [f"- {c['sha'][:7]} {(c.get('commit') or {}).get('message', '').splitlines()[0]}" for c in commits]
    head = f"## Commits ({len(commits)} of {cmp.get('total_commits')})"
    status = "full" if len(commits) >= (cmp.get("total_commits") or 0) else "partial"
    return head + "\n\n" + "\n".join(reversed(lines)) + "\n", {"status": status, "lowest": None}


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
        entry["notes_coverage"] = "none"
        frm, to, unresolved = u["from"], u["to"], []
        if repo and u["ecosystem"] == "github-actions":
            # Compare and read notes between the releases that the floating
            # tags point to today (v4 → v4.4.0, v7 → v7.0.1).
            for side, ref in (("from", from_ref), ("to", to_ref)):
                exact = resolve_floating(repo, ref)
                if exact is None:
                    unresolved.append(ref)
                elif exact != ref:
                    entry[f"{side}_resolved"] = exact
            from_ref = entry.get("from_resolved", from_ref)
            to_ref = entry.get("to_resolved", to_ref)
            frm, to = entry.get("from_resolved", frm), entry.get("to_resolved", to)
            if unresolved:
                entry["unresolved_refs"] = unresolved
        if repo:
            missing = [v for v, r in ((u["from"], from_ref), (u["to"], to_ref)) if not r]
            if missing:
                entry["tags_missing"] = missing
            cmp = gh_json(f"repos/{repo}/compare/{from_ref}...{to_ref}") if from_ref and to_ref else None
            if cmp:
                entry["compare_url"] = cmp.get("html_url")
                entry["commits"] = cmp.get("total_commits")
                entry["files_changed"] = len(cmp.get("files") or [])
                diverged = missing_commits(repo, from_ref, to_ref, cmp)
                if diverged:
                    entry["diverged"] = diverged
            npm_subdir = subdir if u["ecosystem"] in ("npm-ui", "npm-wrapper") else None
            source, text, cov = release_notes(repo, frm, to, to_ref, u["name"], npm_subdir)
            if not text and u["ecosystem"] == "gomod":
                text, cov = commit_log(cmp)
                source = "commit messages" if text else None
            entry["notes_source"] = source
            entry["notes_coverage"] = (cov or {}).get("status", "none")
            # A floating tag that could not be resolved may point past the notes.
            if unresolved and entry["notes_coverage"] == "full":
                entry["notes_coverage"] = "partial"
            entry["notes_lowest"] = (cov or {}).get("lowest")
            for k in ("bump_only", "entries", "reexported"):
                if (cov or {}).get(k):
                    entry[f"notes_{k}"] = cov[k]
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
