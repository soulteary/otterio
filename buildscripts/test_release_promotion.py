"""Offline promotion policy tests; no API calls, publishing or credentials."""
import copy
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("promotion", Path(__file__).with_name("release-promotion.py"))
promotion = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(promotion)
TAG = "RELEASE.2026-10-04T07-00-00Z"
OLDER = "RELEASE.2026-10-03T07-00-00Z"
NEWER = "RELEASE.2026-10-05T07-00-00Z"
SHA = "a" * 40
DIGEST = "sha256:" + "b" * 64


def published(tag, **kwargs):
    return dict(tag_name=tag, draft=False, prerelease=False, **kwargs)


class PromotionTests(unittest.TestCase):
    def setUp(self):
        self.manifest = {"schema_version": 1, "tag": TAG, "source_sha": SHA, "images": [{"repository": "ghcr.io/soulteary/otterio", "digest": DIGEST}]}

    def plan(self, releases=None, **kwargs):
        return promotion.promotion_plan(TAG, self.manifest, releases if releases is not None else [[published(TAG)]], SHA, "soulteary/otterio", **kwargs)

    def test_newest_release_promotes(self):
        self.assertTrue(self.plan([[published(OLDER), published(TAG)]])["promote"])

    def test_older_completion_cannot_roll_back_latest(self):
        self.assertFalse(self.plan([[published(TAG)], [published(NEWER)]])["promote"])

    def test_retry_of_same_digest_is_idempotent(self):
        self.assertEqual(self.plan(), self.plan())
        self.assertEqual(self.plan()["images"][0]["digest"], DIGEST)

    def test_unpublished_candidate_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "not a published stable"):
            self.plan([[published(OLDER)]])

    def test_draft_or_prerelease_candidate_is_rejected(self):
        for field in ("draft", "prerelease"):
            release = published(TAG)
            release[field] = True
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.plan([[release]])

    def test_newer_draft_does_not_block_stable(self):
        newer = published(NEWER)
        newer["draft"] = True
        self.assertTrue(self.plan([[published(TAG), newer]])["promote"])

    def test_tag_commit_mismatch_is_rejected(self):
        for key, value in (("tag", OLDER), ("source_sha", "c" * 40)):
            original = copy.deepcopy(self.manifest)
            self.manifest[key] = value
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "current tag commit"):
                self.plan()
            self.manifest = original

    def test_digest_validation(self):
        for value in ("latest", "sha256:abc", DIGEST + "\n", "sha256:" + "z" * 64):
            self.manifest["images"][0]["digest"] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.plan()

    def test_registry_allowlist(self):
        self.manifest["images"][0]["repository"] = "example.invalid/other/repo"
        with self.assertRaisesRegex(ValueError, "unexpected"):
            self.plan()

    def test_duplicate_repository_is_rejected(self):
        self.manifest["images"].append(dict(self.manifest["images"][0]))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            self.plan()

    def test_dockerhub_requires_configured_identity(self):
        self.manifest["images"].append({"repository": "soulteary/otterio", "digest": DIGEST})
        with self.assertRaises(ValueError):
            self.plan()
        self.assertTrue(self.plan(dockerhub_user="soulteary")["has_dockerhub"])

    def test_invalid_tags(self):
        for value in ("main", "v1.2.3", TAG + "\n", "RELEASE.2026-02-29T00-00-00Z", "RELEASE.$(touch injected)"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                promotion.release_time(value)

    def test_historical_non_timestamp_tags_are_ignored(self):
        self.assertTrue(self.plan([[published(TAG), published("v0.1.0")]])["promote"])

    def test_bad_schema_is_rejected(self):
        self.manifest["schema_version"] = True
        with self.assertRaises(ValueError):
            self.plan()

    def test_newer_release_on_later_api_page_is_seen(self):
        self.assertFalse(self.plan([[published(TAG)], [published(OLDER)], [published(NEWER)]])["promote"])


class ImageTagGuardTests(unittest.TestCase):
    def probe(self, exit_code, error):
        with tempfile.TemporaryDirectory() as directory:
            docker = Path(directory) / "docker"
            docker.write_text('#!/bin/sh\nprintf "%s\\n" "$MOCK_ERROR" >&2\nexit "$MOCK_EXIT"\n')
            docker.chmod(0o755)
            env = dict(os.environ, MOCK_ERROR=error, MOCK_EXIT=str(exit_code))
            env["PATH"] = directory + os.pathsep + env.get("PATH", "")
            return subprocess.run(
                ["bash", str(Path(__file__).with_name("check-new-image-tags.sh")), "ghcr.io/soulteary/otterio:" + TAG],
                env=env, text=True, capture_output=True, timeout=10,
            )

    def test_missing_manifest_allows_new_tag(self):
        self.assertEqual(self.probe(1, "ERROR: manifest unknown").returncode, 0)

    def test_existing_manifest_is_not_overwritten(self):
        result = self.probe(0, "")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Refusing to overwrite", result.stderr)

    def test_auth_failure_does_not_mean_absence(self):
        self.assertNotEqual(self.probe(1, "401 Unauthorized").returncode, 0)

    def test_network_failure_does_not_mean_absence(self):
        self.assertNotEqual(self.probe(1, "TLS handshake timeout").returncode, 0)
        self.assertNotEqual(self.probe(1, "proxy hostname: not found").returncode, 0)


if __name__ == "__main__":
    unittest.main()
