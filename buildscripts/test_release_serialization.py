"""Pin publication/promotion lock scopes in the checked-in workflows, offline.

These are configuration regressions, not an emulator of the Actions scheduler.
They intentionally inspect the small literal concurrency mappings without adding
an external YAML dependency to the existing hermetic release tests.
"""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github/workflows"
GROUP = "otterio-stable-promotion"


def job_block(text, name):
    pattern = rf"(?ms)^  {re.escape(name)}:\n(.*?)(?=^  [\w-]+:\n|\Z)"
    matches = re.findall(pattern, text)
    if len(matches) != 1:
        raise AssertionError(f"expected exactly one {name} job")
    return matches[0]


def literal_concurrency(text, indent):
    prefix = " " * indent
    pattern = rf"(?m)^{prefix}concurrency:\n((?:^{prefix}  [^\n]+\n?)+)"
    matches = re.findall(pattern, text)
    if len(matches) != 1:
        raise AssertionError("expected exactly one literal concurrency mapping at this scope")
    result = {}
    for line in matches[0].splitlines():
        key, value = line.strip().split(":", 1)
        if key in result:
            raise AssertionError("duplicate concurrency key")
        result[key] = value.strip()
    return result


class ReleaseSerializationTests(unittest.TestCase):
    def setUp(self):
        self.release = (WORKFLOWS / "release.yml").read_text(encoding="utf-8")
        self.promotion = (WORKFLOWS / "promote-release.yml").read_text(encoding="utf-8")
        self.publication = (WORKFLOWS / "publish-release.yml").read_text(encoding="utf-8")
        self.stage = job_block(self.release, "release")

    def test_publication_and_promotion_share_one_literal_global_lock(self):
        publication = literal_concurrency(self.publication.split("\njobs:\n")[0], 0)
        promotion = literal_concurrency(self.promotion.split("\njobs:\n")[0], 0)
        self.assertEqual(publication["group"], GROUP)
        self.assertEqual(publication, promotion)
        self.assertEqual(literal_concurrency(self.stage, 4), publication)
        self.assertNotIn("${{", publication["group"])

    def test_global_lock_does_not_cancel_running_or_replace_pending_work(self):
        for text, indent in ((self.stage, 4),
                             (self.publication.split("\njobs:\n")[0], 0),
                             (self.promotion.split("\njobs:\n")[0], 0)):
            with self.subTest(indent=indent):
                concurrency = literal_concurrency(text, indent)
                self.assertEqual(concurrency["cancel-in-progress"], "false")
                self.assertEqual(concurrency["queue"], "max")

    def test_tag_pushes_and_manual_builds_cannot_publish_or_promote(self):
        self.assertEqual(literal_concurrency(self.stage, 4)["group"], GROUP)
        self.assertIsNone(re.search(r"(?m)^    if:", self.stage))
        self.assertNotIn("promote_latest", self.release)
        self.assertNotIn("--draft=false", self.release)
        self.assertNotIn("uses: ./.github/workflows/promote-release.yml", self.release)
        self.assertNotRegex(self.release, r"(?m)^  promote:")

    def test_manual_workflows_do_not_call_each_other_while_holding_the_lock(self):
        outer = literal_concurrency(self.release.split("\njobs:\n")[0], 0)
        self.assertNotEqual(outer["group"], GROUP)
        self.assertIn("release-${{", outer["group"])
        self.assertNotIn("workflow_call:", self.promotion)
        self.assertNotIn("workflow_call:", self.publication)
        self.assertNotRegex(self.release + self.promotion + self.publication,
                            r"uses: \./\.github/workflows/(?:promote|publish)-release.yml")
        self.assertNotIn("concurrency:", job_block(self.promotion, "promote"))
        self.assertNotIn("concurrency:", job_block(self.publication, "publish"))

    def test_builds_remain_outside_the_global_lock(self):
        for name in ("prepare", "binaries", "docker"):
            with self.subTest(job=name):
                self.assertNotIn(GROUP, job_block(self.release, name))
        self.assertIn("needs: [prepare, binaries, docker]", self.stage)

    def test_draft_mutations_are_inside_the_locked_staging_job(self):
        pattern = r"gh release (?:create|upload|edit)\b"
        self.assertEqual(len(re.findall(pattern, self.release)), 3)
        self.assertEqual(len(re.findall(pattern, self.stage)), 3)
        self.assertEqual(literal_concurrency(self.stage, 4)["group"], GROUP)
        self.assertRegex(self.stage, r'gh release edit "\$RELEASE_TAG"[^\n]*\\\n\s+--notes-file "\$RUNNER_TEMP/release-notes.md"')
        self.assertNotIn("--draft=false", self.stage)

    def test_promotion_lock_covers_listing_checks_and_both_alias_writes(self):
        self.assertEqual(literal_concurrency(self.promotion.split("\njobs:\n")[0], 0)["group"], GROUP)
        steps = job_block(self.promotion, "promote")
        listing = steps.index("gh api --paginate --slurp")
        validation = steps.index('python3 buildscripts/release-promotion.py "$RELEASE_TAG" \\\n')
        image_check = steps.index('imagetools inspect "$repository:$RELEASE_TAG"')
        registry_write = steps.index('imagetools create --tag "$repository:latest"')
        github_write = steps.index('gh release edit "$RELEASE_TAG"')
        self.assertLess(listing, validation)
        self.assertLess(validation, image_check)
        self.assertLess(image_check, registry_write)
        self.assertLess(registry_write, github_write)
        self.assertIn("workflow_dispatch:", self.promotion)
        self.assertIn("if: github.ref == 'refs/heads/main'", steps)

    def test_release_checks_discover_this_regression(self):
        checks = (WORKFLOWS / "release-checks.yml").read_text(encoding="utf-8")
        self.assertIn("-p 'test_release_*.py'", checks)


if __name__ == "__main__":
    unittest.main()
