#!/usr/bin/env python3
"""Compares the dependency graph and licenses of base and PR.

Usage:
  deps.py transitive <base-wt> <pr-wt> <ecosystem>
  deps.py licenses   <base-wt> <pr-wt> <ecosystem> <license-policy.json>
ecosystem: gomod | npm-ui | npm-wrapper. Prints JSON.

For Go, the graph is the set of modules compiled into ldcli (go list -deps),
not every module in go.sum. For npm, it is every entry in package-lock.json.
"""
import json
import os
import re
import subprocess
import sys

NPM_DIRS = {"npm-ui": "internal/dev_server/ui", "npm-wrapper": "."}


def run(cmd, cwd):
    out = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(f"{' '.join(cmd)} failed: {out.stderr.strip()[:300]}")
    return out.stdout


def vkey(v):
    nums = re.findall(r"\d+", v or "")[:3]
    return tuple(int(n) for n in nums) + (0,) * (3 - len(nums))


def semver(a, b):
    if not a:
        return "added"
    if not b:
        return "removed"
    x, y = vkey(a), vkey(b)
    if x == y:
        return "none" if a == b else "prerelease"
    if x > y:
        return "downgrade"
    if x[0] != y[0] or (x[0] == 0 and x[1] != y[1]):
        return "major"
    return "minor" if x[1] != y[1] else "patch"


# ---------------------------------------------------------------- Go

def go_built(wt):
    out = run(["go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}", "./..."], wt)
    mods = {}
    for line in out.splitlines():
        if line.strip():
            path, version = line.split()
            mods[path] = version
    return mods


def go_dir(wt, path, version):
    out = run(["go", "mod", "download", "-json", f"{path}@{version}"], wt)
    return json.loads(out).get("Dir")


# ---------------------------------------------------------------- npm

def npm_entries(wt, eco):
    lock = os.path.join(wt, NPM_DIRS[eco], "package-lock.json")
    if not os.path.exists(lock):
        return {}
    with open(lock) as f:
        packages = json.load(f).get("packages", {})
    entries = {}
    for key, meta in packages.items():
        if not key:
            continue
        name = key.rsplit("node_modules/", 1)[-1]
        version = meta.get("version")
        if not version or meta.get("link"):
            continue
        lic = meta.get("license")
        if isinstance(lic, dict):
            lic = lic.get("type")
        entries.setdefault(f"{name}@{version}", {
            "name": name, "version": version, "license": lic,
            "install_script": bool(meta.get("hasInstallScript")), "dev": bool(meta.get("dev")),
        })
    return entries


# ---------------------------------------------------------------- licenses

def identify(text):
    t = re.sub(r"\s+", " ", text.lower())
    if "apache license" in t and "version 2.0" in t:
        return "Apache-2.0"
    if "mozilla public license" in t:
        return "MPL-2.0"
    if "gnu affero general public license" in t:
        return "AGPL-3.0"
    if "gnu lesser general public license" in t:
        return "LGPL"
    if "gnu general public license" in t:
        return "GPL"
    if "permission is hereby granted, free of charge" in t:
        return "MIT"
    if "permission to use, copy, modify, and/or distribute this software for any purpose" in t:
        return "ISC" if "isc" in t or "with or without fee" in t else "0BSD"
    if "redistribution and use in source and binary forms" in t:
        return "BSD-3-Clause" if ("neither the name" in t or "names of its contributors" in t) else "BSD-2-Clause"
    if "this is free and unencumbered software released into the public domain" in t:
        return "Unlicense"
    if "creative commons" in t and "cc0" in t:
        return "CC0-1.0"
    return "unknown"


def go_license(wt, path, version):
    d = go_dir(wt, path, version)
    if not d or not os.path.isdir(d):
        return "unknown", None
    for name in sorted(os.listdir(d)):
        if re.match(r"^(licen[cs]e|copying)(\.(md|txt))?$", name, re.I):
            with open(os.path.join(d, name), errors="replace") as f:
                return identify(f.read()), name
    return "unknown", None


def allowed(expr, policy):
    if not expr or expr == "unknown":
        return False
    expr = expr.strip("() ")
    if " OR " in expr:
        return any(allowed(p, policy) for p in expr.split(" OR "))
    if " AND " in expr:
        return all(allowed(p, policy) for p in expr.split(" AND "))
    return expr in policy["allowed"]


# ---------------------------------------------------------------- commands

def transitive(base, pr, eco):
    if eco == "gomod":
        b, p = go_built(base), go_built(pr)
        changed = [{"name": n, "from": b[n], "to": p[n], "semver": semver(b[n], p[n])}
                   for n in sorted(set(b) & set(p)) if b[n] != p[n]]
        return {
            "ecosystem": eco, "scope": "modules compiled into ldcli",
            "added": [f"{n}@{p[n]}" for n in sorted(set(p) - set(b))],
            "removed": [f"{n}@{b[n]}" for n in sorted(set(b) - set(p))],
            "changed": changed, "install_scripts": [],
        }
    b, p = npm_entries(base, eco), npm_entries(pr, eco)
    names_b = {e["name"] for e in b.values()}
    added = sorted(set(p) - set(b))
    removed = sorted(set(b) - set(p))
    changed = []
    for name in sorted({p[k]["name"] for k in added} & {b[k]["name"] for k in removed}):
        old = sorted(b[k]["version"] for k in removed if b[k]["name"] == name)
        new = sorted(p[k]["version"] for k in added if p[k]["name"] == name)
        changed.append({"name": name, "from": ", ".join(old), "to": ", ".join(new), "semver": semver(old[-1], new[-1])})
    changed_names = {c["name"] for c in changed}
    return {
        "ecosystem": eco, "scope": "package-lock.json",
        "added": [k for k in added if p[k]["name"] not in changed_names],
        "new_packages": sorted({p[k]["name"] for k in added if p[k]["name"] not in names_b}),
        "removed": [k for k in removed if b[k]["name"] not in changed_names],
        "changed": changed,
        "install_scripts": [k for k in added if p[k]["install_script"]],
    }


def licenses(base, pr, eco, policy):
    findings = []
    if eco == "gomod":
        b, p = go_built(base), go_built(pr)
        for name in sorted(p):
            if b.get(name) == p[name]:
                continue
            new, new_file = go_license(pr, name, p[name])
            old = go_license(base, name, b[name])[0] if name in b else None
            findings.append({"package": f"{name}@{p[name]}", "from": old, "to": new, "file": new_file})
    else:
        b, p = npm_entries(base, eco), npm_entries(pr, eco)
        old_by_name = {}
        for e in b.values():
            old_by_name.setdefault(e["name"], e["license"])
        for key in sorted(set(p) - set(b)):
            e = p[key]
            findings.append({"package": key, "from": old_by_name.get(e["name"]), "to": e["license"] or "unknown", "file": None})
    for f in findings:
        f["changed"] = f["from"] is not None and f["from"] != f["to"]
        f["allowed"] = allowed(f["to"], policy)
    return {"ecosystem": eco, "checked": len(findings),
            "problems": [f for f in findings if f["changed"] or not f["allowed"]],
            "all": findings}


def main():
    cmd, base, pr, eco = sys.argv[1:5]
    if cmd == "transitive":
        result = transitive(base, pr, eco)
    elif cmd == "licenses":
        with open(sys.argv[5]) as f:
            result = licenses(base, pr, eco, json.load(f))
    else:
        raise SystemExit(f"unknown command {cmd}")
    json.dump(result, sys.stdout, indent=2)


if __name__ == "__main__":
    main()
