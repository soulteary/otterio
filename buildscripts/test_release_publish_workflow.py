"""Keep publication manual, main-only and bound to the strict acceptance helper."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/publish-release.yml"


class PublishWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.text = WORKFLOW.read_text()

    def test_only_manual_main_dispatch_can_publish(self):
        self.assertIn("  workflow_dispatch:", self.text)
        for event in ("push", "pull_request", "workflow_call", "workflow_run"):
            self.assertNotRegex(self.text, rf"(?m)^  {event}:")
        self.assertIn("    if: github.ref == 'refs/heads/main'", self.text)
        self.assertIn("          ref: main", self.text)
        for name in ("tag", "acceptance_run_id", "attestation_sha256"):
            field = re.search(rf"(?ms)^      {name}:\n(.*?)(?=^      \w+:|\npermissions:)", self.text)
            self.assertIsNotNone(field)
            self.assertIn("        required: true", field[1])

    def test_publication_has_no_build_upload_or_latest_side_effects(self):
        for mutation in ("go build", "docker/build-push-action", "release upload", "release create",
                         "release edit", "--latest", "promote_latest", "promote-release.yml"):
            self.assertNotIn(mutation, self.text)
        self.assertIn("  actions: read", self.text)
        self.assertIn("  contents: write", self.text)
        self.assertNotIn("packages: write", self.text)
        self.assertIn("          GH_TOKEN: ${{ github.token }}", self.text)
        self.assertIn("          CONSOLE_RELEASE_TOKEN: ${{ secrets.CONSOLE_RELEASE_TOKEN }}", self.text)

    def test_exact_helper_inputs_are_quoted_and_failure_propagates(self):
        block = re.search(r"(?m)^        run: \|\n((?:^          .*\n|^\n)+)", self.text)[1]
        script = textwrap.dedent(block)
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            fake = directory / "python3"
            fake.write_text("#!" + sys.executable + "\nimport json,os,sys\n"
                            "open(os.environ['CALLS'],'w').write(json.dumps(sys.argv[1:]))\n"
                            "sys.exit(int(os.environ['EXIT']))\n")
            fake.chmod(0o755)
            env = dict(os.environ, PATH=str(directory) + os.pathsep + os.environ["PATH"],
                       CALLS=str(directory / "calls"), GITHUB_REPOSITORY="soulteary/otterio",
                       RELEASE_TAG="tag with spaces;$(false)", ACCEPTANCE_RUN_ID="12345",
                       ATTESTATION_SHA256="a" * 64)
            for code in (0, 9):
                result = subprocess.run(["bash", "-e", "-c", script], env=dict(env, EXIT=str(code)),
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, code)
                self.assertEqual(json.loads((directory / "calls").read_text()),
                                 ["buildscripts/release-stage.py", "publish", "--repository", "soulteary/otterio",
                                  "--tag", env["RELEASE_TAG"], "--acceptance-run-id", "12345",
                                  "--attestation-sha256", "a" * 64])


if __name__ == "__main__":
    unittest.main()
