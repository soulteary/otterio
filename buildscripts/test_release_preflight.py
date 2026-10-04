"""Hermetic tests: local temporary Git repositories, no network or credentials."""

import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location(
    "release_preflight", Path(__file__).with_name("release-preflight.py")
)
preflight = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(preflight)
TAG = "RELEASE.2026-10-04T07-00-00Z"


class VersionTests(unittest.TestCase):
    def test_utc_version_without_duplicate_prefix(self):
        self.assertEqual(preflight.release_version(TAG), "2026-10-04T07:00:00Z")

    def test_leap_day(self):
        self.assertEqual(
            preflight.release_version("RELEASE.2028-02-29T23-59-59Z"),
            "2028-02-29T23:59:59Z",
        )

    def test_invalid_tags(self):
        for tag in (
            "", "v1.2.3", "main", "refs/tags/" + TAG, TAG + "\n", TAG + ".hotfix",
            "RELEASE.2026-02-29T00-00-00Z", "RELEASE.2026-13-04T00-00-00Z",
            "RELEASE.2026-10-04T24-00-00Z", "RELEASE.2026-10-04T07-60-00Z",
            "RELEASE.2026-10-04T07-00-60Z", "RELEASE.2026-10-04T07:00:00Z",
            "RELEASE.$(touch INJECTED)", TAG + "\nsha=bad", "--help",
        ):
            with self.subTest(tag=tag), self.assertRaises(preflight.PreflightError):
                preflight.release_version(tag)


class RepositoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "release-test@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "tag.gpgsign", "false")
        (self.root / "RELEASE_NOTES.md").write_text("# Reviewed release notes\n")
        self.commit()
        self.head = self.git("rev-parse", "HEAD")
        self.git("update-ref", "refs/remotes/origin/main", self.head)

    def git(self, *args):
        return subprocess.run(
            ["git", *args], cwd=self.root, check=True, capture_output=True, text=True
        ).stdout.strip()

    def commit(self):
        self.git("add", "-A")
        self.git("commit", "-m", "test fixture")

    def check(self, existing=False):
        return preflight.check_release(TAG, self.root, existing)

    def test_new_tag_check_is_read_only(self):
        self.assertEqual(self.check()["sha"], self.head)
        self.assertEqual(self.git("tag", "--list"), "")
        self.assertEqual(self.git("status", "--porcelain"), "")

    def test_untracked_file_rejected(self):
        (self.root / "unexpected").write_text("uncommitted")
        with self.assertRaisesRegex(preflight.PreflightError, "dirty"):
            self.check()

    def test_modified_file_rejected(self):
        (self.root / "RELEASE_NOTES.md").write_text("changed")
        with self.assertRaisesRegex(preflight.PreflightError, "dirty"):
            self.check()

    def test_wrong_branch_rejected(self):
        self.git("switch", "-c", "topic")
        with self.assertRaisesRegex(preflight.PreflightError, "release from main"):
            self.check()

    def test_local_commit_ahead_rejected(self):
        (self.root / "new").write_text("new")
        self.commit()
        with self.assertRaisesRegex(preflight.PreflightError, "origin/main"):
            self.check()

    def test_missing_origin_main_rejected(self):
        self.git("update-ref", "-d", "refs/remotes/origin/main")
        with self.assertRaises(preflight.PreflightError):
            self.check()

    def test_existing_tag_cannot_be_reused(self):
        self.git("tag", TAG)
        with self.assertRaisesRegex(preflight.PreflightError, "already exists"):
            self.check()

    def test_missing_tag_rejected_in_ci(self):
        with self.assertRaises(preflight.PreflightError):
            self.check(existing=True)

    def test_lightweight_tag_in_ci(self):
        self.git("tag", TAG)
        self.git("checkout", "--detach", TAG)
        self.assertEqual(self.check(existing=True)["sha"], self.head)

    def test_annotated_tag_peels_to_commit(self):
        self.git("tag", "-a", TAG, "-m", "release")
        self.git("checkout", "--detach", TAG)
        self.assertEqual(self.check(existing=True)["sha"], self.head)

    def test_mismatched_checkout_rejected(self):
        self.git("tag", TAG)
        (self.root / "new").write_text("new")
        self.commit()
        with self.assertRaisesRegex(preflight.PreflightError, "does not match"):
            self.check(existing=True)

    def test_tag_outside_main_rejected(self):
        self.git("switch", "-c", "topic")
        (self.root / "new").write_text("new")
        self.commit()
        self.git("tag", TAG)
        with self.assertRaises(preflight.PreflightError):
            self.check(existing=True)

    def test_main_may_advance_after_tag_in_ci(self):
        self.git("tag", TAG)
        (self.root / "new").write_text("new")
        self.commit()
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")
        self.git("checkout", "--detach", TAG)
        self.assertEqual(self.check(existing=True)["sha"], self.head)

    def test_notes_required(self):
        for text in (None, "  \n"):
            with self.subTest(text=text):
                notes = self.root / "RELEASE_NOTES.md"
                if text is None:
                    notes.unlink()
                else:
                    notes.write_text(text)
                self.commit()
                self.git("update-ref", "refs/remotes/origin/main", "HEAD")
                with self.assertRaisesRegex(preflight.PreflightError, "RELEASE_NOTES"):
                    self.check()


if __name__ == "__main__":
    unittest.main()
