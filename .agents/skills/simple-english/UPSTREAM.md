# Source of this skill

This folder holds a copy of the Simple English skill from [AminBlg/SimpleEnglish](https://github.com/AminBlg/SimpleEnglish), at commit `79b590fc8596523d92c26b1ea7e33236606ef069`. The project uses the MIT license. The license text is in `LICENSE`.

## Files from the upstream project

These files are copies of upstream files:

| File in this folder | File in the upstream project |
| --- | --- |
| `SKILL.md` | `skills/simple-english/SKILL.md` |
| `references/rule-catalog.md` | `skills/simple-english/references/rule-catalog.md` |
| `references/strict-vocabulary.md` | `skills/simple-english/references/strict-vocabulary.md` |
| `references/use-cases.md` | `skills/simple-english/references/use-cases.md` |
| `references/word-swaps.md` | `skills/simple-english/references/word-swaps.md` |
| `references/system-prompt.md` | `prompts/system-prompt.md` |
| `scripts/ste_lint.py` | `evals/ste_lint.py` |
| `scripts/slop.tsv` | `evals/slop.tsv` |
| `LICENSE` | `LICENSE` |

Two copies have a change:

- In `scripts/ste_lint.py`, the `strip_code` function keeps the newlines that it removes. In the upstream version, each line number after a code block or a table is too small. The hook compares these line numbers with `git diff`, so the numbers must be correct. The change does not change the violation counts.
- In `references/rule-catalog.md`, the blank line at the end of the file is removed. The `end-of-file-fixer` pre-commit hook of this repository requires one newline at the end of each file.

## Files for this repository

`scripts/hook.py` is the hook script for Cursor, Claude Code, and Codex. It replaces the two upstream hook scripts, `src/hooks/simple-english-activate.js` and `src/hooks/lint_hook.py`. The upstream scripts read the layout of a plugin, and they support Claude Code and Codex only. `scripts/test_hook.py` holds the tests for `scripts/hook.py`.

## Update the copy

1. Copy the upstream files in the table above into this folder.
2. Apply the two changes above again, if the upstream files still need them.
3. Change the commit at the top of this file.
4. Run `python3 .agents/skills/simple-english/scripts/test_hook.py`.
5. Run `python3 .agents/skills/simple-english/scripts/ste_lint.py --self-test`.
