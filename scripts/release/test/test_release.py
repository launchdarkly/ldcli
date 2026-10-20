"""Unit tests for scripts/release/lib. Run: scripts/release/test/run.sh"""

import json
import os
import shutil
import sys
import tempfile
import unittest

LIB = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "lib")
sys.path.insert(0, LIB)

import cli_surface  # noqa: E402
import inventory  # noqa: E402
import plan as planlib  # noqa: E402
import verdict  # noqa: E402

RISK_MAP = inventory.RiskMap(json.load(open(os.path.join(LIB, "..", "risk-map.json"))))


def commit(short, subject, files, body=""):
    c = {"sha": short + "0" * 33, "short": short, "subject": subject, "body": body, "files": files,
         "release_commit": inventory.RELEASE_SUBJECT.match(subject) is not None, "reverts": None,
         "lines_added": 1, "lines_deleted": 1}
    c.update(inventory.parse_subject(subject))
    return c


class InventoryTest(unittest.TestCase):
    def test_parse_subject(self):
        s = inventory.parse_subject("fix: Add local flag overrides using dev server (#837) (#850)")
        self.assertEqual((s["type"], s["pr"], s["mentions"]), ("fix", 850, [837]))
        s = inventory.parse_subject("feat(sync)!: drop the old flag (#9)")
        self.assertEqual((s["scope"], s["breaking"], s["description"]), ("sync", True, "drop the old flag"))
        self.assertIsNone(inventory.parse_subject("Update README")["type"])

    def test_glob(self):
        self.assertTrue(inventory.glob_to_re("**/*.md").match("README.md"))
        self.assertTrue(inventory.glob_to_re("**/*.md").match("docs/a/b.md"))
        self.assertFalse(inventory.glob_to_re("cmd/*").match("cmd/a/b.go"))
        self.assertEqual(RISK_MAP.area_of("internal/sync/prompt/x.go")["id"], "prompt-sync")
        self.assertEqual(RISK_MAP.area_of("internal/dev_server/ui/src/App.tsx")["id"], "dev-server-ui")
        self.assertEqual(RISK_MAP.area_of(".github/workflows/release-please.yml")["id"], "release-workflows")
        self.assertEqual(RISK_MAP.area_of("AGENTS.md")["id"], "agent-tooling")

    def test_changelog(self):
        text = ("# Changelog\n\n## [3.13.0](x) (2026-10-19)\n\n### Features\n\n"
                "* add x ([#1](u)) ([abcdef1](u))\n\n### Bug Fixes\n\n* fix y ([#2](u)) ([1234567](u))\n\n"
                "## [3.12.0](x)\n\n* old ([#0](u)) ([0000000](u))\n")
        cl = inventory.parse_changelog(text, "3.13.0")
        self.assertEqual(sorted(cl), ["Bug Fixes", "Features"])
        self.assertEqual(cl["Features"][0]["shas"], ["abcdef1"])
        self.assertIsNone(inventory.parse_changelog(text, "9.9.9"))

    def test_revert_and_hidden_change_flags(self):
        feat = commit("aaaaaaa", "feat: add overrides (#837)", ["internal/dev_server/db/sqlite.go"])
        rev = commit("bbbbbbb", "fix: add overrides (#837) (#850)", ["internal/dev_server/db/sqlite.go"])
        rev["reverts"] = feat["sha"]
        dep = commit("ccccccc", "chore(deps): update api-client-go to v24 (#832)", ["go.mod", "internal/client/client.go"])
        tooling = commit("ddddddd", "chore: tweak hooks (#1)", [".cursor/hooks.json"])
        rel = commit("eeeeeee", "chore(main): release 3.13.0", ["CHANGELOG.md"])
        changelog = {"Features": [{"shas": ["aaaaaaa"]}], "Bug Fixes": [{"shas": ["bbbbbbb"]}]}
        commits = [feat, rev, dep, tooling, rel]
        flags = inventory.classify(commits, RISK_MAP, changelog)
        kinds = sorted((f["kind"], f["commit"]) for f in flags)
        self.assertIn(("changelog-lists-reverted-commit", "aaaaaaa"), kinds)
        self.assertIn(("revert-listed-as-change", "bbbbbbb"), kinds)
        self.assertIn(("shipped-change-not-in-changelog", "ccccccc"), kinds)
        self.assertNotIn(("shipped-change-not-in-changelog", "ddddddd"), kinds)
        units = {u["id"]: u["commits"] for u in inventory.propose_units(commits, RISK_MAP)}
        self.assertEqual(units["dev-server-data"], ["aaaaaaa", "bbbbbbb"])
        self.assertEqual(units["deps-pr-832"], ["ccccccc"])
        self.assertNotIn("eeeeeee", sum(units.values(), []))


class CliSurfaceTest(unittest.TestCase):
    BASE = """### ldcli
Flags:
      --access-token string   token
      --base-uri string       URI (default "https://app.launchdarkly.com")
### ldcli dev-server start
Optional flags:
      --port string   port (default "8765")
      --old-flag      removed later
Global flags:
      --access-token string   token
"""
    CAND = """### ldcli
Flags:
      --access-token string   token
      --base-uri string       URI (default "https://app.launchdarkly.com")
### ldcli dev-server start
Optional flags:
      --port string   port
                      that wraps (default
                      "9000")
      --host string   host (default "127.0.0.1")
Global flags:
      --access-token string   token
### ldcli sync
"""

    def test_compare(self):
        out = cli_surface.compare(self.BASE, self.CAND)
        self.assertEqual(out["commands_added"], ["sync"])
        self.assertEqual(out["flags_added"], ["ldcli dev-server start --host"])
        self.assertEqual(out["flags_removed"], ["ldcli dev-server start --old-flag"])
        self.assertEqual(out["defaults_changed"], ["ldcli dev-server start --port: '8765' to '9000'"])
        self.assertEqual(out["types_changed"], [])


class Fixture:
    """A temporary out dir with an inventory, a plan, and check results."""

    def __init__(self):
        self.dir = tempfile.mkdtemp()
        commits = [
            {"short": "aaaaaaa", "subject": "fix(dev-server): listen on 127.0.0.1", "risk": "high", "pr": 845},
            {"short": "bbbbbbb", "subject": "chore: docs", "risk": "low", "pr": 1},
            {"short": "ccccccc", "subject": "chore(main): release 3.13.0", "risk": "low", "release_commit": True},
        ]
        self.write("inventory.json", {"schema": 1, "commits": commits, "counts": {"commits": 2}, "flags": []})
        self.write("state/pr.json", {"version": "3.13.0", "since": "v3.12.0", "head_sha": "c" * 40})
        self.baseline("go-test", "pass", required=True, gate=True)
        self.plan([
            {"id": "dev", "commits": ["aaaaaaa"], "risk": "high",
             "claims": [{"id": "loopback", "text": "listens on loopback", "checks": ["dev-loopback"]}]},
            {"id": "docs", "commits": ["bbbbbbb"], "risk": "low",
             "claims": [{"id": "ok", "text": "builds", "baseline": ["go-test"]}]},
        ], [
            {"id": "dev-loopback", "unit": "dev", "claims": ["loopback"], "kind": "discriminating",
             "adversarial": True, "script": "dev-loopback.sh"},
        ])
        self.result("dev-loopback", "base", "fail")
        self.result("dev-loopback", "pr", "pass")

    def write(self, rel, data):
        path = os.path.join(self.dir, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            if isinstance(data, str):
                f.write(data)
            else:
                json.dump(data, f)

    def baseline(self, cid, status, required=False, gate=False, base=None, fingerprint=None):
        order = os.path.join(self.dir, "state", "check-order.txt")
        os.makedirs(os.path.dirname(order), exist_ok=True)
        with open(order, "a") as f:
            f.write(cid + "\n")
        self.write(f"state/checks/{cid}/entry.json", {"id": cid, "title": cid, "required": required, "gate": gate})
        self.write(f"state/checks/{cid}/pr/result.json", {"status": status, "summary": "s", "fingerprint": fingerprint,
                                                         "question": "Q?" if status == "decide" else None})
        if base:
            self.write(f"state/checks/{cid}/base/result.json", {"status": base, "summary": "s", "fingerprint": fingerprint})

    def plan(self, units, checks):
        self.write("agent/plan.json", {"schema": 1, "units": units})
        self.write("generated/checks.json", {"schema": 1, "checks": checks})
        for c in checks:
            self.write(f"generated/{c['script']}", "#!/bin/sh\n")

    def result(self, cid, side, status, **extra):
        self.write(f"state/generated/{cid}/{side}/result.json", dict({"status": status, "summary": f"{side} {status}"}, **extra))

    def judge(self, checks=None, **extra):
        self.write("agent/judgement.json", dict({"schema": 1, "judge": "test",
                                                 "checks": checks if checks is not None else [{"id": "dev-loopback", "assessment": "sound"}]}, **extra))

    def build(self):
        return verdict.build(self.dir)

    def close(self):
        shutil.rmtree(self.dir)


class VerdictTest(unittest.TestCase):
    def setUp(self):
        self.f = Fixture()

    def tearDown(self):
        self.f.close()

    def test_outcome_table(self):
        o = verdict.check_outcome
        s = lambda x: {"status": x}  # noqa: E731
        self.assertEqual(o("discriminating", s("fail"), s("pass"), None), "proven")
        self.assertEqual(o("discriminating", s("skip"), s("pass"), s("fail")), "proven-by-revert")
        self.assertEqual(o("discriminating", s("fail"), s("pass"), s("pass")), "not-discriminating")
        self.assertEqual(o("discriminating", s("pass"), s("fail"), None), "regression")
        self.assertEqual(o("discriminating", s("fail"), s("fail"), None), "fails-both")
        self.assertEqual(o("guard", s("skip"), s("pass"), None), "holds")
        self.assertEqual(o("guard", s("fail"), s("pass"), None), "invalid")
        self.assertEqual(o("guard", s("pass"), s("error"), None), "incomplete")
        self.assertEqual(o("guard", None, s("pass"), None), "not-run")

    def test_safe_with_judge(self):
        self.f.judge()
        r = self.f.build()
        self.assertEqual(r["verdict"], "safe-to-release", r["incomplete"] + r["blocks"])
        self.assertEqual(r["units"][1]["claims"][0]["covered_by"], ["baseline:go-test"])

    def test_no_judge_is_incomplete(self):
        r = self.f.build()
        self.assertEqual(r["verdict"], "incomplete")
        self.assertTrue(any("judge" in i["text"] for i in r["incomplete"]))

    def test_no_plan_is_incomplete(self):
        os.remove(os.path.join(self.f.dir, "agent", "plan.json"))
        self.assertEqual(self.f.build()["verdict"], "incomplete")

    def test_vacuous_proof_does_not_count(self):
        self.f.judge([{"id": "dev-loopback", "assessment": "vacuous", "reason": "reads help text"}])
        r = self.f.build()
        self.assertEqual(r["verdict"], "incomplete")
        self.assertEqual(r["units"][0]["claims"][0]["status"], "not-covered")

    def test_regression_blocks_unless_false_positive(self):
        self.f.result("dev-loopback", "base", "pass")
        self.f.result("dev-loopback", "pr", "fail")
        self.f.judge([{"id": "dev-loopback", "assessment": "confirmed-bug"}])
        r = self.f.build()
        self.assertEqual(r["verdict"], "block")
        self.assertEqual(r["units"][0]["claims"][0]["status"], "disproved")
        self.assertEqual(r["incomplete"], [])
        self.f.judge([{"id": "dev-loopback", "assessment": "false-positive"}])
        r = self.f.build()
        self.assertEqual(r["verdict"], "incomplete")
        self.assertFalse(r["blocks"])

    def test_fails_both_needs_a_confirmed_bug_to_block(self):
        self.f.result("dev-loopback", "pr", "fail")
        self.f.judge([{"id": "dev-loopback", "assessment": "unclear"}])
        self.assertEqual(self.f.build()["verdict"], "incomplete")
        self.f.judge([{"id": "dev-loopback", "assessment": "confirmed-bug"}])
        self.assertEqual(self.f.build()["verdict"], "block")

    def test_unsafe_check_and_gap_are_incomplete(self):
        self.f.judge([{"id": "dev-loopback", "assessment": "unsafe", "reason": "writes to live"}])
        self.assertEqual(self.f.build()["verdict"], "incomplete")
        self.f.judge(gaps=[{"unit": "dev", "text": "new flag"}])
        r = self.f.build()
        self.assertEqual(r["verdict"], "incomplete")
        self.assertTrue(any("new flag" in i["text"] for i in r["incomplete"]))

    def test_no_local_proof_is_a_question(self):
        self.f.plan([
            {"id": "dev", "commits": ["aaaaaaa"], "risk": "high",
             "claims": [{"id": "loopback", "text": "listens on loopback", "checks": ["dev-loopback"]},
                        {"id": "workflow", "text": "release workflow runs", "no_local_proof": "only the release runs it"}]},
            {"id": "docs", "commits": ["bbbbbbb"], "risk": "low", "claims": [{"id": "ok", "text": "x", "baseline": ["go-test"]}]},
        ], [{"id": "dev-loopback", "unit": "dev", "claims": ["loopback"], "kind": "discriminating",
             "adversarial": True, "script": "dev-loopback.sh"}])
        self.f.judge()
        r = self.f.build()
        self.assertEqual(r["verdict"], "needs-human")
        self.assertIn("release workflow runs", r["decisions"][0]["question"])

    def test_baseline_rules(self):
        self.f.baseline("go-generate-drift", "fail", required=True, base="fail", fingerprint="x")
        self.f.judge()
        r = self.f.build()
        self.assertEqual(r["verdict"], "safe-to-release")
        self.assertTrue(any("last release had the same failure" in n["text"] for n in r["notes"]))
        self.f.baseline("go-build-vet", "fail", required=True, gate=True, base="fail", fingerprint="y")
        self.assertEqual(self.f.build()["verdict"], "block")

    def test_blocking_finding_needs_judge_review(self):
        self.f.plan([
            {"id": "dev", "commits": ["aaaaaaa"], "risk": "high",
             "claims": [{"id": "loopback", "text": "x", "checks": ["dev-loopback"]}],
             "findings": [{"severity": "block", "text": "changelog is wrong", "fix": "override"}]},
            {"id": "docs", "commits": ["bbbbbbb"], "risk": "low", "claims": [{"id": "ok", "text": "x", "baseline": ["go-test"]}]},
        ], [{"id": "dev-loopback", "unit": "dev", "claims": ["loopback"], "kind": "discriminating",
             "adversarial": True, "script": "dev-loopback.sh"}])
        self.f.judge(findings=[{"unit": "dev", "index": 0, "assessment": "rejected", "reason": "not true"}])
        self.assertEqual(self.f.build()["verdict"], "safe-to-release")
        self.f.judge(findings=[{"unit": "dev", "index": 0, "assessment": "confirmed"}])
        self.assertEqual(self.f.build()["verdict"], "block")

    def test_finding_that_explains_a_block_replaces_it(self):
        self.f.baseline("changelog-accuracy", "fail", required=True)
        self.f.result("dev-loopback", "base", "pass")
        self.f.result("dev-loopback", "pr", "fail")
        self.f.plan([
            {"id": "dev", "commits": ["aaaaaaa"], "risk": "high",
             "claims": [{"id": "loopback", "text": "x", "checks": ["dev-loopback"]}],
             "findings": [
                 {"severity": "block", "text": "the bug, explained", "fix": "f", "evidence": ["logs/pr/generated-dev-loopback.log"]},
                 {"severity": "block", "text": "the changelog, explained", "fix": "g", "about": ["baseline:changelog-accuracy"]},
                 {"severity": "decide", "question": "Ship X?", "text": "t", "evidence": ["logs/pr/generated-dev-loopback-other.log"]}]},
            {"id": "docs", "commits": ["bbbbbbb"], "risk": "low", "claims": [{"id": "ok", "text": "x", "baseline": ["go-test"]}]},
        ], [{"id": "dev-loopback", "unit": "dev", "claims": ["loopback"], "kind": "discriminating",
             "adversarial": True, "script": "dev-loopback.sh"}])
        self.f.judge([{"id": "dev-loopback", "assessment": "confirmed-bug"}],
                     findings=[{"unit": "dev", "index": i, "assessment": "confirmed"} for i in range(3)])
        r = self.f.build()
        self.assertEqual([b["text"] for b in r["blocks"]], ["the bug, explained", "the changelog, explained"])
        self.assertEqual([d["question"] for d in r["decisions"]], ["Ship X?"])


class PlanTest(unittest.TestCase):
    def setUp(self):
        self.f = Fixture()

    def tearDown(self):
        self.f.close()

    def problems(self):
        return [p["text"] for p in planlib.validate(self.f.dir)["problems"]]

    def test_valid_plan(self):
        self.assertEqual(self.problems(), [])

    def test_coverage_risk_and_adversarial(self):
        self.f.plan([
            {"id": "dev", "commits": ["aaaaaaa", "bbbbbbb"], "risk": "medium",
             "claims": [{"id": "loopback", "text": "x", "checks": ["dev-loopback"]}]},
            {"id": "docs", "commits": ["bbbbbbb"], "risk": "high", "claims": [{"id": "ok", "text": "x"}]},
        ], [{"id": "dev-loopback", "unit": "dev", "claims": ["loopback"], "kind": "guard", "script": "dev-loopback.sh"}])
        p = " ".join(self.problems())
        self.assertIn("Commit bbbbbbb is in two units", p)
        self.assertIn("has risk medium, but its commits are high risk", p)
        self.assertIn("Unit docs is high risk, but it has no adversarial check", p)
        self.assertIn("Claim docs/ok has no check", p)

    def test_missing_commit(self):
        self.f.plan([{"id": "dev", "commits": ["aaaaaaa"], "risk": "high",
                      "claims": [{"id": "loopback", "text": "x", "checks": ["dev-loopback"]}]}],
                    [{"id": "dev-loopback", "unit": "dev", "claims": ["loopback"], "kind": "discriminating",
                      "adversarial": True, "script": "dev-loopback.sh"}])
        self.assertTrue(any("bbbbbbb" in p and "in no unit" in p for p in self.problems()))

    def test_fragments_merge(self):
        self.f.plan([
            {"id": "dev", "commits": ["aaaaaaa"], "risk": "high"},
            {"id": "docs", "commits": ["bbbbbbb"], "risk": "low", "claims": [{"id": "ok", "text": "x", "baseline": ["go-test"]}]},
        ], [])
        self.f.write("agent/units/dev.json", {
            "claims": [{"id": "loopback", "text": "x", "checks": ["dev-loopback"]}],
            "checks": [{"id": "dev-loopback", "claims": ["loopback"], "kind": "discriminating",
                        "adversarial": True, "script": "dev-loopback.sh"}]})
        self.f.write("generated/dev-loopback.sh", "#!/bin/sh\n")
        self.assertEqual(self.problems(), [])
        _, manifest = planlib.load(self.f.dir)
        self.assertEqual(manifest["checks"][0]["unit"], "dev")
        self.f.judge()
        self.assertEqual(self.f.build()["verdict"], "safe-to-release")


if __name__ == "__main__":
    unittest.main()
