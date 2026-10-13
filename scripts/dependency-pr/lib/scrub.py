#!/usr/bin/env python3
"""Removes access tokens from check logs and evidence files, in place.

Usage: scrub.py <file>...
Literal secrets come from the VERIFY_SECRETS environment variable, one per
line. Known token formats and credentials in URLs are removed too.
"""
import os
import re
import sys

MASK = "[REDACTED]"
PATTERNS = [
    (re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})"), MASK),
    (re.compile(r"\b(?:api|sdk|mob)-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b"), MASK),
    # https://user:secret@host and https://secret@host
    (re.compile(r"([a-z][a-z0-9+.-]*://)[^/\s:@'\"]+(?::[^/\s@'\"]*)?@"), r"\1" + MASK + "@"),
    (re.compile(r"(?i)(authorization:\s*(?:bearer|token|basic)\s+)[^\s'\"]+"), r"\1" + MASK),
]
MAX_BYTES = 20 * 1024 * 1024


def secrets():
    return sorted({s.strip() for s in os.environ.get("VERIFY_SECRETS", "").splitlines() if len(s.strip()) >= 8},
                  key=len, reverse=True)


def scrub(text, literals):
    for s in literals:
        text = text.replace(s, MASK)
    for rx, repl in PATTERNS:
        text = rx.sub(repl, text)
    return text


def main():
    literals = secrets()
    for path in sys.argv[1:]:
        try:
            if not os.path.isfile(path) or os.path.islink(path) or os.path.getsize(path) > MAX_BYTES:
                continue
            with open(path, "rb") as f:
                raw = f.read()
            text = raw.decode("utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        clean = scrub(text, literals)
        if clean != text:
            with open(path, "w", encoding="utf-8") as f:
                f.write(clean)


if __name__ == "__main__":
    main()
