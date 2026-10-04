"""Execute the actual workflow guards with a fake gh and real jq, offline.

The stub records mutations and uses local fixture assets. It never contacts
GitHub or a registry. No third-party Python or YAML dependency is needed.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/release.yml"
TAG = "RELEASE.2026-10-04T07-00-00Z"
PREPARE = "Reject published releases and prerelease drafts before building"
PUBLISH = "Upload only to a draft and verify downloaded assets"


def workflow_shell(name):
    lines = WORKFLOW.read_text().splitlines()
    start = lines.index("      - name: " + name) + 1
    while start < len(lines) and lines[start] != "        run: |":
        if lines[start].startswith("      - "):
            raise AssertionError("named step has no shell block")
        start += 1
    result = []
    for line in lines[start + 1:]:
        if line and not line.startswith("          "):
            break
        result.append(line[10:] if line else "")
    if not result:
        raise AssertionError("empty workflow shell")
    return "\n".join(result)


STUB = r'''
import json, os, pathlib, shutil, subprocess, sys
args = sys.argv[1:]
with open(os.environ["CALL_LOG"], "a") as log:
    log.write(json.dumps(args) + "\n")
if args[0] == "api":
    if os.environ.get("API_FAIL") == "1":
        print("fixture: network/authentication failure", file=sys.stderr)
        sys.exit(1)
    for page in json.loads(os.environ["RELEASE_PAGES"]):
        if "--jq" in args:
            query = args[args.index("--jq") + 1]
            subprocess.run(["jq", "-r", query], input=json.dumps(page), text=True, check=True)
        else:
            print(json.dumps(page))
elif args[:2] == ["release", "download"]:
    destination = pathlib.Path(args[args.index("--dir") + 1])
    for source in pathlib.Path("dist").iterdir():
        shutil.copyfile(source, destination / source.name)
    if os.environ.get("CORRUPT_ASSET") == "1":
        (destination / "fixture.bin").write_bytes(b"corrupt")
elif args[:2] not in (["release", "create"], ["release", "upload"], ["release", "edit"]):
    raise SystemExit("unexpected gh fixture invocation: " + repr(args))
'''


def draft(**updates):
    return dict(tag_name=TAG, draft=True, prerelease=False, **updates)


class ReleaseDraftStateTests(unittest.TestCase):
    def run_step(self, name, pages, **settings):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "dist").mkdir()
            (root / "dist/fixture.bin").write_bytes(b"verified fixture")
            (root / "RELEASE_NOTES.md").write_text("fixture notes\n")
            stub = root / "gh"
            stub.write_text("#!" + sys.executable + "\n" + STUB)
            stub.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ.get("PATH", ""),
                       RUNNER_TEMP=str(root), TMPDIR=str(root), CALL_LOG=str(root / "calls"),
                       GITHUB_REPOSITORY="soulteary/otterio", GH_TOKEN="test-only",
                       RELEASE_TAG=TAG, RELEASE_SHA="a" * 40,
                       RELEASE_PAGES=json.dumps(pages), **settings)
            result = subprocess.run(["bash", "-c", workflow_shell(name)], cwd=root,
                                    env=env, text=True, capture_output=True, timeout=10)
            calls = [json.loads(line) for line in (root / "calls").read_text().splitlines()]
            return result, calls

    def assert_refused_without_mutation(self, name, pages, **settings):
        result, calls = self.run_step(name, pages, **settings)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(all(call[0] == "api" for call in calls), calls)

    def test_prepare_allows_absent_release(self):
        result, calls = self.run_step(PREPARE, [[]])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(all(call[0] == "api" for call in calls))

    def test_prepare_allows_stable_draft(self):
        result, _ = self.run_step(PREPARE, [[draft()]])
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_prepare_rejects_prerelease_draft(self):
        self.assert_refused_without_mutation(PREPARE, [[dict(draft(), prerelease=True)]])

    def test_prepare_rejects_published_releases(self):
        for prerelease in (False, True):
            with self.subTest(prerelease=prerelease):
                self.assert_refused_without_mutation(PREPARE, [[dict(draft(), draft=False, prerelease=prerelease)]])

    def test_prepare_requires_explicit_boolean_state(self):
        for field in ("draft", "prerelease"):
            value = draft()
            del value[field]
            with self.subTest(field=field):
                self.assert_refused_without_mutation(PREPARE, [[value]])

    def test_prepare_checks_all_pages(self):
        unrelated = dict(draft(), tag_name="RELEASE.2026-09-01T00-00-00Z")
        self.assert_refused_without_mutation(PREPARE, [[unrelated], [], [dict(draft(), prerelease=True)]])

    def test_publish_rejects_prerelease_before_upload(self):
        self.assert_refused_without_mutation(PUBLISH, [[dict(draft(), prerelease=True)]])

    def test_publish_rejects_missing_state(self):
        for field in ("draft", "prerelease"):
            value = draft()
            del value[field]
            with self.subTest(field=field):
                self.assert_refused_without_mutation(PUBLISH, [[value]])

    def test_publish_rejects_already_published(self):
        self.assert_refused_without_mutation(PUBLISH, [[dict(draft(), draft=False)]])

    def test_publish_creates_and_verifies_a_new_draft(self):
        result, calls = self.run_step(PUBLISH, [[]])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual([c[1] for c in calls if c[0] == "release"],
                         ["create", "upload", "download", "edit"])
        create, edit = calls[1], calls[-1]
        self.assertIn("--draft", create)
        self.assertIn("--draft=false", edit)
        self.assertIn("--latest=false", edit)

    def test_publish_resumes_only_a_stable_draft(self):
        result, calls = self.run_step(PUBLISH, [[draft()]])
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual([c[1] for c in calls if c[0] == "release"], ["upload", "download", "edit"])

    def test_publish_rejects_ambiguous_multiple_matches(self):
        self.assert_refused_without_mutation(PUBLISH, [[dict(draft(), prerelease=True)], [draft()]])

    def test_api_failure_is_not_a_missing_release(self):
        for name in (PREPARE, PUBLISH):
            with self.subTest(step=name):
                self.assert_refused_without_mutation(name, [[]], API_FAIL="1")

    def test_asset_mismatch_prevents_publication(self):
        result, calls = self.run_step(PUBLISH, [[draft()]], CORRUPT_ASSET="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(["release", "edit"], [c[:2] for c in calls])

    def test_state_is_rechecked_after_preparation(self):
        result, _ = self.run_step(PREPARE, [[]])
        self.assertEqual(result.returncode, 0)
        self.assert_refused_without_mutation(PUBLISH, [[], [dict(draft(), prerelease=True)]])


if __name__ == "__main__":
    if not shutil.which("jq"):
        raise SystemExit("jq is required to test the actual workflow predicates")
    unittest.main()
