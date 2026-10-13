#!/usr/bin/env python3
"""Tests for hook.py. Run: python3 .agents/skills/simple-english/scripts/test_hook.py"""
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

HOOK = pathlib.Path(__file__).resolve().parent / "hook.py"
REPO_ROOT = HOOK.parent.parent.parent.parent.parent

BAD_MARKDOWN = "You should run the command; it's important.\n"
GOOD_MARKDOWN = "Run the command. The command deletes old rows.\n"


def run(event_name, harness, payload=None, env=None):
    full_env = {k: v for k, v in os.environ.items() if not k.startswith(("CURSOR_", "SIMPLE_ENGLISH_"))}
    full_env.update(env or {})
    stdin = "" if payload is None else (payload if isinstance(payload, str) else json.dumps(payload))
    return subprocess.run(
        [sys.executable, str(HOOK), event_name, "--harness", harness],
        input=stdin,
        capture_output=True,
        text=True,
        env=full_env,
        cwd=REPO_ROOT,
        timeout=30,
    )


class MarkdownFile:
    """A temporary Markdown file inside the repository, because the hook skips files outside it."""

    def __init__(self, text):
        self.text = text

    def __enter__(self):
        self.dir = tempfile.TemporaryDirectory(dir=REPO_ROOT, prefix="simple-english-test-")
        self.path = pathlib.Path(self.dir.name, "doc.md")
        self.path.write_text(self.text, encoding="utf-8")
        return self.path

    def __exit__(self, *exc):
        self.dir.cleanup()


class SessionStartTest(unittest.TestCase):
    def test_cursor_gets_additional_context_json(self):
        result = run("session-start", "cursor", {"hook_event_name": "sessionStart"})
        self.assertEqual(result.returncode, 0)
        context = json.loads(result.stdout)["additional_context"]
        self.assertIn("SIMPLE ENGLISH RULES FOR THIS REPOSITORY", context)
        self.assertIn("THE REPLY", context)
        self.assertLess(len(context), 9500)

    def test_claude_and_codex_get_plain_text(self):
        for harness in ("claude", "codex"):
            result = run("session-start", harness, {"hook_event_name": "SessionStart"})
            self.assertEqual(result.returncode, 0)
            self.assertTrue(result.stdout.startswith("SIMPLE ENGLISH RULES FOR THIS REPOSITORY"))
            self.assertIn("THE DOCUMENT", result.stdout)

    def test_claude_hook_does_nothing_when_cursor_runs_it(self):
        from_stdin = run("session-start", "claude", {"hook_event_name": "sessionStart", "cursor_version": "2.0.0"})
        from_env = run("session-start", "claude", {}, env={"CURSOR_VERSION": "2.0.0"})
        for result in (from_stdin, from_env):
            self.assertEqual(result.returncode, 0)
            self.assertEqual(result.stdout, "")

    def test_off_switch(self):
        result = run("session-start", "cursor", {}, env={"SIMPLE_ENGLISH_HOOKS": "off"})
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")


class PostEditTest(unittest.TestCase):
    def test_cursor_reports_violations_as_additional_context(self):
        with MarkdownFile(BAD_MARKDOWN) as path:
            result = run("post-edit", "cursor", {"tool_name": "Write", "tool_input": {"file_path": str(path)}})
        self.assertEqual(result.returncode, 0)
        report = json.loads(result.stdout)["additional_context"]
        self.assertIn("doc.md has", report)
        self.assertIn("banned_modal", report)

    def test_cursor_accepts_a_relative_path_and_a_string_tool_input(self):
        with MarkdownFile(BAD_MARKDOWN) as path:
            relative = path.relative_to(REPO_ROOT).as_posix()
            result = run("post-edit", "cursor", {"tool_input": json.dumps({"path": relative}), "cwd": str(REPO_ROOT)})
        self.assertIn("additional_context", json.loads(result.stdout))

    def test_claude_reports_violations_on_stderr_with_exit_2(self):
        with MarkdownFile(BAD_MARKDOWN) as path:
            result = run("post-edit", "claude", {"hook_event_name": "PostToolUse", "tool_input": {"file_path": str(path)}})
        self.assertEqual(result.returncode, 2)
        self.assertIn("contraction", result.stderr)
        self.assertEqual(result.stdout, "")

    def test_clean_file_gives_no_output(self):
        with MarkdownFile(GOOD_MARKDOWN) as path:
            result = run("post-edit", "cursor", {"tool_input": {"file_path": str(path)}})
        self.assertEqual((result.returncode, result.stdout), (0, ""))

    def test_skips_files_that_are_not_linted(self):
        skipped = [
            REPO_ROOT / "main.go",
            REPO_ROOT / "CHANGELOG.md",
            REPO_ROOT / ".agents/skills/simple-english/SKILL.md",
            pathlib.Path(tempfile.gettempdir(), "outside-the-repository.md"),
        ]
        for path in skipped:
            result = run("post-edit", "cursor", {"tool_input": {"file_path": str(path)}})
            self.assertEqual((result.returncode, result.stdout), (0, ""), path)


class ChangedLinesTest(unittest.TestCase):
    """For a committed file, the lint reports only the lines that differ from the last commit."""

    def setUp(self):
        self.dir = tempfile.TemporaryDirectory(prefix="simple-english-repo-")
        self.root = pathlib.Path(self.dir.name)
        self.doc = self.root / "doc.md"
        self.doc.write_text(BAD_MARKDOWN, encoding="utf-8")
        git = ["git", "-C", str(self.root), "-c", "user.name=test", "-c", "user.email=test@example.com"]
        subprocess.run([*git, "init", "-q"], check=True)
        subprocess.run([*git, "add", "doc.md"], check=True)
        subprocess.run([*git, "commit", "-q", "-m", "Add doc"], check=True)
        self.env = {"SIMPLE_ENGLISH_REPO_ROOT": str(self.root)}

    def tearDown(self):
        self.dir.cleanup()

    def edit(self, text):
        self.doc.write_text(BAD_MARKDOWN + text, encoding="utf-8")
        return run("post-edit", "cursor", {"tool_input": {"file_path": str(self.doc)}}, env=self.env)

    def test_ignores_violations_in_lines_that_did_not_change(self):
        result = self.edit(GOOD_MARKDOWN)
        self.assertEqual((result.returncode, result.stdout), (0, ""))

    def test_reports_violations_in_a_changed_line(self):
        result = self.edit("You'll see that it has been removed.\n")
        report = json.loads(result.stdout)["additional_context"]
        self.assertIn("in the lines that you changed", report)
        self.assertIn("line 2,", report)
        self.assertNotIn("line 1,", report)

    def test_line_numbers_are_correct_after_code_blocks_and_tables(self):
        blocks = "\n```bash\nmake build\nmake test\n```\n\n| Name | Value |\n| --- | --- |\n| a | b |\n\n"
        result = self.edit(blocks + "You should not do this.\n")
        report = json.loads(result.stdout)["additional_context"]
        bad_line = (BAD_MARKDOWN + blocks).count("\n") + 1
        self.assertIn(f"line {bad_line}, banned_modal: should", report)


class StopTest(unittest.TestCase):
    def test_reports_formatting_in_a_reply(self):
        result = run("stop", "claude", {"hook_event_name": "Stop", "last_assistant_message": "**Yes** — it works.\n\n- one\n- two\n"})
        self.assertEqual(result.returncode, 0)
        message = json.loads(result.stdout)["systemMessage"]
        self.assertIn("bold span", message)
        self.assertIn("em dash", message)

    def test_prose_reply_gives_no_output(self):
        result = run("stop", "claude", {"last_assistant_message": "The build passed. You can merge the change."})
        self.assertEqual((result.returncode, result.stdout), (0, ""))


class BadInputTest(unittest.TestCase):
    def test_malformed_or_missing_input_never_fails(self):
        for event_name in ("session-start", "post-edit", "stop"):
            for payload in ("not json", "[]", ""):
                result = run(event_name, "claude", payload)
                self.assertEqual(result.returncode, 0, (event_name, payload))


if __name__ == "__main__":
    unittest.main()
