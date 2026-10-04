"""Manifest and real workflow-step regressions; no network or credentials."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "buildscripts/release-promotion.py"
WORKFLOW = ROOT / ".github/workflows/release.yml"
SPEC = importlib.util.spec_from_file_location("release_manifest", SCRIPT)
promotion = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(promotion)
TAG = "RELEASE.2026-10-04T18-48-37Z"
SHA = "a" * 40
DIGEST = "sha256:" + "b" * 64


def step_script(name):
    """Extract one checked-in literal run block without a YAML dependency."""
    matches = re.findall(
        rf"(?ms)^      - name: {re.escape(name)}\n(.*?)(?=^      - |\Z)",
        WORKFLOW.read_text(encoding="utf-8"),
    )
    if len(matches) != 1:
        raise AssertionError("expected one workflow step: " + name)
    run = re.search(r"(?m)^        run: \|\n((?:^          .*\n|^\n)+)", matches[0])
    if run is None:
        raise AssertionError("expected a literal run block: " + name)
    return textwrap.dedent(run[1])


class ManifestTests(unittest.TestCase):
    def build(self, **kwargs):
        args = dict(tag=TAG, source_sha=SHA, repository="soulteary/otterio",
                    digest=DIGEST, dockerhub_published="false", dockerhub_user="")
        args.update(kwargs)
        return promotion.build_manifest(**args)

    def test_ghcr_only(self):
        self.assertEqual(self.build(), {
            "schema_version": 1, "tag": TAG, "source_sha": SHA,
            "images": [{"repository": "ghcr.io/soulteary/otterio", "digest": DIGEST}],
        })

    def test_same_username_as_owner_records_both_registries(self):
        manifest = self.build(dockerhub_published="true", dockerhub_user="soulteary")
        self.assertEqual([image["repository"] for image in manifest["images"]],
                         ["ghcr.io/soulteary/otterio", "soulteary/otterio"])
        self.assertTrue(all(image["digest"] == DIGEST for image in manifest["images"]))

    def test_dockerhub_identity_is_not_assumed_to_be_the_github_owner(self):
        manifest = self.build(repository="GitHub-Owner/OtterIO",
                              dockerhub_published="true", dockerhub_user="docker-owner")
        self.assertEqual([image["repository"] for image in manifest["images"]],
                         ["ghcr.io/github-owner/otterio", "docker-owner/otterio"])

    def test_configured_username_without_a_push_does_not_add_dockerhub(self):
        self.assertEqual(len(self.build(dockerhub_user="soulteary")["images"]), 1)

    def test_missing_or_invalid_publication_status_fails_closed(self):
        for value in (None, "", "TRUE", "0", True, False, "false\n"):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "publication status"):
                self.build(dockerhub_published=value)

    def test_published_dockerhub_requires_a_valid_local_identity(self):
        for value in ("", "SoulTeary", "owner/other", " owner", "owner\n", "***", None):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "Docker Hub username"):
                self.build(dockerhub_published="true", dockerhub_user=value)

    def test_missing_or_malformed_build_digest_is_rejected(self):
        for value in (None, "", "latest", "sha256:abc", DIGEST + "\n", "sha256:" + "z" * 64):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "image digest"):
                self.build(digest=value)

    def test_invalid_release_identity_is_rejected_before_writing(self):
        for key, values in {
            "tag": ("", "main", TAG + "\n"),
            "source_sha": (None, "", "a" * 39, SHA + "\n"),
            "repository": (None, "", "owner", "ghcr.io/owner/repo", "owner/repo\n"),
        }.items():
            for value in values:
                with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                    self.build(**{key: value})

    def test_generated_manifests_pass_unchanged_promotion_policy(self):
        for status, user in (("false", ""), ("true", "soulteary"), ("true", "docker-owner")):
            with self.subTest(status=status, user=user):
                manifest = self.build(dockerhub_published=status, dockerhub_user=user)
                before = copy.deepcopy(manifest)
                plan = promotion.promotion_plan(
                    TAG, manifest, [[dict(tag_name=TAG, draft=False, prerelease=False)]],
                    SHA, "soulteary/otterio", user,
                )
                self.assertTrue(plan["promote"])
                self.assertEqual(plan["has_dockerhub"], status == "true")
                self.assertEqual(manifest, before)

    def test_empty_or_masked_legacy_repository_is_still_rejected(self):
        for value in ("", "ghcr.io/***/otterio", None):
            manifest = self.build()
            manifest["images"][0]["repository"] = value
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "unexpected"):
                promotion.validate_manifest(TAG, manifest, SHA, "soulteary/otterio")

    def test_cli_writes_only_after_validation_and_preserves_schema(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "release-manifest.json"
            command = [sys.executable, str(SCRIPT), TAG, "--write-manifest", str(output),
                       "--source-sha", SHA, "--repository", "soulteary/otterio",
                       "--dockerhub-published", "false", "--digest"]
            result = subprocess.run(command + [DIGEST], capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(output.read_text()), self.build())
            self.assertTrue(output.read_bytes().endswith(b"\n"))
            original = output.read_bytes()
            result = subprocess.run(command + [""], capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(output.read_bytes(), original)
            output.unlink()
            result = subprocess.run(command + [""], capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(output.exists())


class WorkflowManifestTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        helpers = self.root / "buildscripts"
        helpers.mkdir()
        shutil.copyfile(SCRIPT, helpers / SCRIPT.name)
        # The tag-selection step still runs its guard, but no registry is queried.
        (helpers / "check-new-image-tags.sh").write_text(
            '#!/bin/sh\nprintf "%s\\n" "$@" > "$MOCK_REFERENCES"\n'
        )
        dist = self.root / "dist"
        dist.mkdir()
        payload = b"offline release binary fixture\n"
        (dist / "otterio-linux-amd64").write_bytes(payload)
        (dist / ("otterio-" + TAG + "-checksums.txt")).write_text(
            hashlib.sha256(payload).hexdigest() + "  otterio-linux-amd64\n"
        )
        self.output = self.root / "outputs"
        self.env = dict(os.environ, GITHUB_REPOSITORY="SoulTeary/OtterIO",
                        RELEASE_TAG=TAG, RELEASE_SHA=SHA, IMAGE_DIGEST=DIGEST,
                        DOCKERHUB_USERNAME="soulteary", DOCKERHUB_TOKEN="offline-fixture-token",
                        GITHUB_OUTPUT=str(self.output), MOCK_REFERENCES=str(self.root / "references"),
                        # Simulate both legacy cross-job outputs being omitted.
                        GHCR_REPOSITORY="", DOCKERHUB_REPOSITORY="")

    def run_step(self, name):
        return subprocess.run(["bash", "-e", "-c", step_script(name)], cwd=self.root,
                              env=self.env, text=True, capture_output=True, timeout=10)

    def select_tags(self):
        result = self.run_step("Compute immutable image tags")
        self.assertEqual(result.returncode, 0, result.stderr)
        return dict(line.split("=", 1) for line in self.output.read_text().splitlines())

    def write_manifest(self):
        return self.run_step("Verify release assets and record immutable image identities")

    def test_secret_overlap_does_not_drop_either_repository(self):
        for user in ("soulteary", "docker-owner"):
            with self.subTest(user=user):
                self.env["DOCKERHUB_USERNAME"] = user
                outputs = self.select_tags()
                self.env["DOCKERHUB_PUBLISHED"] = outputs["dockerhub_published"]
                result = self.write_manifest()
                self.assertEqual(result.returncode, 0, result.stderr)
                manifest = json.loads((self.root / "dist/release-manifest.json").read_text())
                expected = ["ghcr.io/soulteary/otterio", user + "/otterio"]
                self.assertEqual([item["repository"] for item in manifest["images"]], expected)
                self.assertEqual((self.root / "references").read_text().splitlines(),
                                 [name + ":" + TAG for name in expected])
                self.assertEqual(outputs["tags"], ",".join(name + ":" + TAG for name in expected))

    def test_optional_dockerhub_tracks_actual_build_selection(self):
        for user, token in (("", ""), ("soulteary", ""), ("", "offline-fixture-token")):
            with self.subTest(user=user, token_present=bool(token)):
                self.env.update(DOCKERHUB_USERNAME=user, DOCKERHUB_TOKEN=token)
                outputs = self.select_tags()
                self.assertEqual(outputs["dockerhub_published"], "false")
                self.env["DOCKERHUB_PUBLISHED"] = outputs["dockerhub_published"]
                result = self.write_manifest()
                self.assertEqual(result.returncode, 0, result.stderr)
                manifest = json.loads((self.root / "dist/release-manifest.json").read_text())
                self.assertEqual(manifest["images"],
                                 [{"repository": "ghcr.io/soulteary/otterio", "digest": DIGEST}])

    def test_missing_build_outputs_abort_before_an_asset_is_created(self):
        for key in ("DOCKERHUB_PUBLISHED", "IMAGE_DIGEST"):
            with self.subTest(key=key):
                self.env.update(DOCKERHUB_PUBLISHED="true", IMAGE_DIGEST=DIGEST)
                self.env[key] = ""
                result = self.write_manifest()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.root / "dist/release-manifest.json").exists())

    def test_cross_job_wiring_does_not_transport_repository_names(self):
        workflow = WORKFLOW.read_text(encoding="utf-8")
        docker = re.search(r"(?ms)^  docker:\n(.*?)(?=^  [\w-]+:\n)", workflow)[1]
        outputs = re.search(r"(?ms)^    outputs:\n(.*?)(?=^    env:\n)", docker)[1]
        self.assertNotIn("ghcr_repository:", outputs)
        self.assertNotIn("dockerhub_repository:", outputs)
        self.assertIn("digest: ${{ steps.image.outputs.digest }}", outputs)
        self.assertIn("dockerhub_published: ${{ steps.tags.outputs.dockerhub_published }}", outputs)
        self.assertNotIn("needs.docker.outputs.ghcr_repository", workflow)
        self.assertNotIn("needs.docker.outputs.dockerhub_repository", workflow)
        self.assertIn("DOCKERHUB_PUBLISHED: ${{ needs.docker.outputs.dockerhub_published }}", workflow)
        consumer = workflow[workflow.index("      - name: Verify release assets and record immutable image identities"):]
        self.assertIn("DOCKERHUB_USERNAME: ${{ secrets.DOCKERHUB_USERNAME }}", consumer)
        self.assertLess(consumer.index("--write-manifest"), consumer.index("gh release upload"))


if __name__ == "__main__":
    unittest.main()
