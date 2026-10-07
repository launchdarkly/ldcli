#!/usr/bin/env python3
"""Simple English hooks for Cursor, Claude Code, and Codex.

The hooks are advisory. They never block an action, and a failure in this
script never stops a session.

Usage: hook.py EVENT --harness HARNESS

  session-start  Send the Simple English rules to the agent.
  post-edit      Lint a Markdown file that the agent wrote.
  stop           Check the last reply for formatting (Claude Code only).

HARNESS is cursor, claude, or codex. It selects the input and output format.

The lint reports only the lines that differ from the last commit, so that an
agent does not rewrite text that its task did not touch. A new file gets a full
lint.

Set SIMPLE_ENGLISH_HOOKS=off to turn off all the hooks. The tests set
SIMPLE_ENGLISH_REPO_ROOT to use a different repository.
"""
import argparse
import fnmatch
import json
import os
import pathlib
import re
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
SKILL_DIR = HERE.parent
REPO_ROOT = pathlib.Path(os.environ.get("SIMPLE_ENGLISH_REPO_ROOT") or SKILL_DIR.parent.parent.parent)
RULES_FILE = SKILL_DIR / "references" / "system-prompt.md"
sys.path.insert(0, str(HERE))
# The hook runs in every checkout. Do not leave __pycache__ folders in the working tree.
sys.dont_write_bytecode = True

# Claude Code writes hook output over 10,000 characters to a file and sends only a preview.
MAX_CONTEXT_CHARS = 9500
MAX_LINT_HITS = 12

# Paths relative to the repository root. The file check does not lint these files.
SKIP_PATTERNS = (
    ".agents/skills/simple-english/*",
    "CHANGELOG.md",
    "*/node_modules/*",
    "vendor/*",
)

HEADER = """SIMPLE ENGLISH RULES FOR THIS REPOSITORY

Write all prose in Simple English: replies, Markdown files, pull request descriptions, commit messages, and code comments. The rules follow. The full skill, with the rule catalog and the check mode, is at .agents/skills/simple-english/SKILL.md. Read it before you write or rewrite a document.

"""

FALLBACK_RULES = (
    "Use short sentences, active voice, and simple tenses. Put a condition before its command. "
    "Use one word for one meaning. Do not change code, identifiers, commands, or quoted errors."
)

OPENERS = re.compile(r"^\s*(certainly|great question|you're absolutely right|sure[,!]|absolutely[,!])", re.I)
CLOSERS = re.compile(r"(i hope this helps|let me know if|feel free to)", re.I)


def rule_block(text):
    """The rules between the first two "---" lines of the prompt file."""
    parts = re.split(r"^---[ \t]*$", text, flags=re.M)
    return parts[1].strip() if len(parts) >= 3 else text.strip()


def session_context():
    try:
        rules = rule_block(RULES_FILE.read_text(encoding="utf-8"))
    except OSError:
        rules = FALLBACK_RULES
    context = HEADER + rules
    if len(context) > MAX_CONTEXT_CHARS:
        context = HEADER + FALLBACK_RULES
    return context


def load_linter():
    try:
        import ste_lint  # noqa: WPS433

        return ste_lint
    except Exception:  # noqa: BLE001
        return None


def called_by_cursor(event):
    """Cursor also runs the hooks in .claude/settings.json. Cursor runs its own copy from .cursor/hooks.json."""
    return bool(event.get("cursor_version") or os.environ.get("CURSOR_VERSION"))


def edited_path(event):
    tool_input = event.get("tool_input") or {}
    if isinstance(tool_input, str):
        try:
            tool_input = json.loads(tool_input)
        except ValueError:
            return None
    if not isinstance(tool_input, dict):
        return None
    for key in ("file_path", "path", "target_file", "filePath"):
        value = tool_input.get(key)
        if isinstance(value, str) and value:
            return value
    return event.get("file_path") or None


def lint_target(event, raw_path):
    """The absolute path of the Markdown file to lint, or None to skip the file."""
    if not raw_path or not raw_path.endswith(".md"):
        return None
    base = event.get("cwd") or os.environ.get("CURSOR_PROJECT_DIR") or os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
    target = pathlib.Path(base, pathlib.Path(raw_path).expanduser()).resolve()
    try:
        relative = target.relative_to(REPO_ROOT.resolve()).as_posix()
    except ValueError:
        return None
    if any(fnmatch.fnmatch(relative, pattern) for pattern in SKIP_PATTERNS):
        return None
    return target


HUNK = re.compile(r"^@@ -\S+ \+(\d+)(?:,(\d+))? @@", re.M)


def git(*args):
    return subprocess.run(["git", "-C", str(REPO_ROOT), *args], capture_output=True, text=True, timeout=5)


def changed_lines(target):
    """The line numbers that differ from the last commit. None means all lines."""
    try:
        diff = git("diff", "--no-color", "--unified=0", "HEAD", "--", str(target))
        if diff.returncode != 0:
            return None
        if not diff.stdout:
            tracked = git("ls-files", "--error-unmatch", "--", str(target))
            return set() if tracked.returncode == 0 else None
    except (OSError, subprocess.SubprocessError):
        return None
    lines = set()
    for match in HUNK.finditer(diff.stdout):
        start, count = int(match.group(1)), int(match.group(2) or 1)
        lines.update(range(start, start + count))
    return lines


def lint_report(target):
    """A short report of the violations in the changed lines, or None when they have none."""
    lint = load_linter()
    if lint is None:
        return None
    try:
        text = target.read_text(encoding="utf-8")
    except OSError:
        return None
    hits = lint.lint_detail(text, "descriptive")
    changed = changed_lines(target)
    if changed is not None:
        hits = [hit for hit in hits if hit["line"] in changed]
    if not hits:
        return None
    scope = "in the file" if changed is None else "in the lines that you changed"
    lines = [f"simple-english: {target.name} has {len(hits)} Simple English violations {scope}."]
    for hit in hits[:MAX_LINT_HITS]:
        lines.append(f"  line {hit['line']}, {hit['category']}: {hit['text']}")
    if len(hits) > MAX_LINT_HITS:
        lines.append(f"  {len(hits) - MAX_LINT_HITS} more violations are not shown.")
    lines.append("Correct these violations in the lines that you wrote. Do not change code, quoted text, or other lines.")
    return "\n".join(lines)


def reply_problems(reply):
    problems = []
    lint = load_linter()
    if lint is not None:
        counts = lint.reader_check(reply)["counts"]
        for key, label in (("em_dash", "em dash"), ("bold_spans", "bold span"), ("headers", "header"), ("bullets", "list item")):
            if counts[key]:
                problems.append(f"{counts[key]} {label}(s)")
        prose = re.sub(r"```.*?```", " ", reply, flags=re.S)
        prose = re.sub(r"`[^`]*`", " ", prose)
        slop = lint.lint(prose, "descriptive")["violations"].get("slop_word", 0)
        if slop:
            problems.append(f"{slop} slop word(s)")
    if OPENERS.search(reply):
        problems.append("a filler opener")
    if CLOSERS.search(reply):
        problems.append("a filler closer")
    return problems


def session_start(harness, event):
    context = session_context()
    if harness == "cursor":
        print(json.dumps({"additional_context": context}))
    else:
        sys.stdout.write(context)
    return 0


def post_edit(harness, event):
    target = lint_target(event, edited_path(event))
    report = lint_report(target) if target else None
    if not report:
        return 0
    if harness == "cursor":
        print(json.dumps({"additional_context": report}))
        return 0
    # Claude Code shows stderr to the model when a PostToolUse hook exits with 2. The tool already ran.
    sys.stderr.write(report + "\n")
    return 2


def stop(harness, event):
    problems = reply_problems(event.get("last_assistant_message") or "")
    if problems:
        message = "simple-english reply check: " + ", ".join(problems) + ". Answer in prose."
        print(json.dumps({"systemMessage": message}))
    return 0


EVENTS = {"session-start": session_start, "post-edit": post_edit, "stop": stop}


def main(argv=None):
    parser = argparse.ArgumentParser(description="Simple English hooks.")
    parser.add_argument("event", choices=sorted(EVENTS))
    parser.add_argument("--harness", choices=("cursor", "claude", "codex"), required=True)
    args = parser.parse_args(argv)

    if os.environ.get("SIMPLE_ENGLISH_HOOKS", "").lower() == "off":
        return 0
    try:
        raw = sys.stdin.read() if not sys.stdin.isatty() else ""
        event = json.loads(raw) if raw.strip() else {}
    except ValueError:
        event = {}
    if not isinstance(event, dict):
        event = {}
    if args.harness == "claude" and called_by_cursor(event):
        return 0
    try:
        return EVENTS[args.event](args.harness, event)
    except Exception:  # noqa: BLE001  An advisory hook must never block or loop a session.
        return 0


if __name__ == "__main__":
    sys.exit(main())
