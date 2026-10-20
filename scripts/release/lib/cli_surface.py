#!/usr/bin/env python3
"""Compares the CLI surface (commands, flags, defaults) of two help dumps.

Usage: cli_surface.py <base all.txt> <candidate all.txt>   (prints JSON)

A help dump is the all.txt that help_dump in scripts/dependency-pr/lib/check.sh
writes: one "### ldcli <command>" section for each command. Global flags are
compared only on the root command.
"""

import json
import re
import sys

SECTION = re.compile(r"^### ldcli ?(.*)$")
FLAG = re.compile(r"^\s+(?:-(\w), )?--([A-Za-z0-9][A-Za-z0-9-]*)(?: (\S+))?(?:\s{2,}(.*))?$")
HEADING = re.compile(r"^(\S.*):\s*$")
DEFAULT = re.compile(r"\(default (\"[^\"]*\"|\[[^\]]*\]|[^)]*)\)")


def parse(text):
    """Returns {command: {flag: {"short", "type", "default", "help"}}}."""
    commands, cmd, heading, last = {}, None, "", None
    for line in text.splitlines():
        m = SECTION.match(line)
        if m:
            cmd = m.group(1).strip()
            commands.setdefault(cmd, {})
            heading, last = "", None
            continue
        if cmd is None:
            continue
        h = HEADING.match(line)
        if h:
            heading, last = h.group(1).lower(), None
            continue
        if "global" in heading and cmd != "":
            last = None
            continue
        f = FLAG.match(line)
        if f and "flag" in heading:
            short, name, typ, desc = f.groups()
            if name == "help":
                last = None
                continue
            last = {"short": short, "type": typ, "help": (desc or "").strip()}
            commands[cmd][name] = last
            continue
        if last is not None and line.startswith(" ") and line.strip():
            last["help"] = (last["help"] + " " + line.strip()).strip()
        elif not line.strip():
            last = None
    for flags in commands.values():
        for meta in flags.values():
            d = DEFAULT.search(meta["help"])
            meta["default"] = d.group(1).strip('"') if d else None
    return commands


def compare(base, cand):
    b, c = parse(base), parse(cand)
    out = {
        "commands_added": sorted(set(c) - set(b)),
        "commands_removed": sorted(set(b) - set(c)),
        "flags_added": [], "flags_removed": [], "defaults_changed": [], "types_changed": [],
    }
    for cmd in sorted(set(b) & set(c)):
        bf, cf = b[cmd], c[cmd]
        label = f"ldcli {cmd}".strip()
        for name in sorted(set(cf) - set(bf)):
            out["flags_added"].append(f"{label} --{name}")
        for name in sorted(set(bf) - set(cf)):
            out["flags_removed"].append(f"{label} --{name}")
        for name in sorted(set(bf) & set(cf)):
            if bf[name]["default"] != cf[name]["default"]:
                out["defaults_changed"].append(
                    f"{label} --{name}: {bf[name]['default']!r} to {cf[name]['default']!r}")
            if bf[name]["type"] != cf[name]["type"]:
                out["types_changed"].append(
                    f"{label} --{name}: {bf[name]['type'] or 'bool'} to {cf[name]['type'] or 'bool'}")
    out["commands"] = {"base": len(b), "candidate": len(c)}
    return out


if __name__ == "__main__":
    with open(sys.argv[1]) as f1, open(sys.argv[2]) as f2:
        print(json.dumps(compare(f1.read(), f2.read()), indent=2))
