"""Exercise immutable release-tag creation and refusal paths without writes."""
import importlib.util
import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("prepare_tag", ROOT / "buildscripts/prepare-release-tag.py")
PREPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREPARE)
SHA = "a" * 40
TAG = "RELEASE.2026-10-22T06-00-00Z"
REPOSITORY = "soulteary/otterio"


class PrepareTagTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.existing = False
        self.failed_ci = False
        self.moved_main = False
        self.env = patch.dict(os.environ, {"GITHUB_REPOSITORY": REPOSITORY,
                              "GITHUB_REF": "refs/heads/main", "GITHUB_SHA": SHA})
        self.env.start()
        self.addCleanup(self.env.stop)
        self.command = patch.object(PREPARE, "command", side_effect=self.fake_command)
        self.command.start()
        self.addCleanup(self.command.stop)

    def fake_command(self, *args):
        self.calls.append(args)
        if args == ("git", "rev-parse", "HEAD") or args == ("git", "rev-parse", "refs/remotes/origin/main"):
            return SHA
        if args == ("git", "status", "--porcelain"):
            return ""
        if args[:3] == ("git", "tag", "--list"):
            return TAG if self.existing else ""
        if len(args) > 2 and "actions/workflows/" in args[2]:
            return json.dumps({"workflow_runs": [{"head_sha": SHA, "head_branch": "main", "event": "push",
                              "status": "completed", "conclusion": "failure" if self.failed_ci else "success"}]})
        if args == ("gh", "api", f"repos/{REPOSITORY}/git/ref/heads/main"):
            return json.dumps({"object": {"sha": "b" * 40 if self.moved_main else SHA}})
        return ""

    def assert_no_write(self):
        self.assertFalse(any("POST" in call or "PATCH" in call or call[:3] == ("gh", "workflow", "run")
                             for call in self.calls))

    def test_invalid_tag_cannot_reach_commands(self):
        with self.assertRaises(PREPARE.PREFLIGHT.PreflightError):
            PREPARE.prepare("../main")
        self.assertEqual(self.calls, [])

    def test_non_main_cannot_create_tag(self):
        os.environ["GITHUB_REF"] = "refs/heads/untrusted"
        with self.assertRaises(ValueError):
            PREPARE.prepare(TAG)
        self.assert_no_write()

    def test_existing_tag_is_never_reused(self):
        self.existing = True
        with self.assertRaises(ValueError):
            PREPARE.prepare(TAG)
        self.assert_no_write()

    def test_unsuccessful_ci_prevents_tag_creation(self):
        self.failed_ci = True
        with self.assertRaises(ValueError):
            PREPARE.prepare(TAG)
        self.assert_no_write()

    def test_concurrent_main_change_prevents_tag_creation(self):
        self.moved_main = True
        with self.assertRaises(ValueError):
            PREPARE.prepare(TAG)
        self.assert_no_write()

    def test_success_creates_only_new_ref_then_dispatches_tag_workflow(self):
        PREPARE.prepare(TAG)
        writes = [call for call in self.calls if "POST" in call]
        self.assertEqual(len(writes), 1)
        self.assertIn(f"ref=refs/tags/{TAG}", writes[0])
        self.assertIn(f"sha={SHA}", writes[0])
        self.assertFalse(any("PATCH" in call for call in self.calls))
        self.assertEqual(self.calls[-1], ("gh", "workflow", "run", "release.yml", "--repo", REPOSITORY,
                                         "--ref", TAG, "-f", f"tag={TAG}"))


if __name__ == "__main__":
    unittest.main()
