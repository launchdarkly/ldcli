#!/usr/bin/env python3
"""Makes rendered comment Markdown safe for GitHub, outside code spans and
code blocks:

- A bare @-token (`@launchpad-ui/core`, `@types/react-window`, `@v4`) goes in
  backticks, because GitHub turns it into a user or team mention.
- Raw angle text (`flags:<NOUNDEFS|PIE>`, `<1.2.0`) goes in backticks, because
  GitHub reads it as HTML. The comment's own tags (details, summary, sub, br,
  and HTML comments) stay.

Usage: comment_safe.py < in.md > out.md
       comment_safe.py --check FILE   (exit 1 and list the problems)
"""
import re
import sys

ALLOWED_TAGS = re.compile(r"^</?(details|summary|sub|br)\b[^>]*>$|^<!--.*-->$", re.S)
MENTION = re.compile(
    r"(?:^|(?<=[\s(\[{,;:!?\"'*|]))@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?"
    r"(?:/[A-Za-z0-9_.-]*[A-Za-z0-9_])?(?:@[0-9A-Za-z^~*.-]*[0-9A-Za-z*])?", re.M)
ANGLE = re.compile(r"<!--.*?-->|<(?=[0-9=])[^\s),|]*[^\s),.;:|]|<[^<>\n]*>", re.S)


def mask_code(text):
    """The text with code blocks and code spans replaced by spaces."""
    blank = lambda m: re.sub(r"[^\n]", " ", m.group(0))
    text = re.sub(r"^```.*?^```[ \t]*$", blank, text, flags=re.S | re.M)
    return re.sub(r"(`+)(?:(?!\1)[^\n])+?\1", blank, text)


def spans(text):
    body = mask_code(text)
    found = [(m.start(), m.end(), "bare mention") for m in MENTION.finditer(body)]
    found += [(m.start(), m.end(), "raw angle text") for m in ANGLE.finditer(body) if not ALLOWED_TAGS.match(m.group(0))]
    return sorted(found)


def safe(text):
    for s, e, _ in reversed(spans(text)):
        text = text[:s] + "`" + text[s:e] + "`" + text[e:]
    return text


if __name__ == "__main__":
    if len(sys.argv) > 2 and sys.argv[1] == "--check":
        raw = open(sys.argv[2], encoding="utf-8").read()
        found = spans(raw)
        for s, e, kind in found:
            print(f"{raw.count(chr(10), 0, s) + 1}: {kind}: {raw[s:e]}")
        sys.exit(1 if found else 0)
    sys.stdout.write(safe(sys.stdin.read()))
