"""Run metadata generation outside a module/Git checkout; no network required."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("gen-ldflags.go").resolve()


class BuildMetadataTests(unittest.TestCase):
    def run_generator(self, commit):
        env = dict(os.environ, GO111MODULE="off", GOTOOLCHAIN="local")
        for name in ("OTTERIO_BUILD_COMMIT", "OTTERIO_RELEASE", "OTTERIO_HOTFIX"):
            env.pop(name, None)
        if commit is not None:
            env["OTTERIO_BUILD_COMMIT"] = commit
        with tempfile.TemporaryDirectory() as directory:
            return subprocess.run(
                ["go", "run", str(SCRIPT), "2026-10-04T00:00:00Z"],
                cwd=directory, env=env, text=True, capture_output=True, timeout=60,
            )

    def test_explicit_commit_without_git_directory(self):
        sha = "be06908a3608809368756f24b19f55642a399cb5"
        result = self.run_generator(sha)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("cmd.CommitID=" + sha, result.stdout)
        self.assertIn("cmd.ShortCommitID=" + sha[:12], result.stdout)
        self.assertIn("cmd.ReleaseTag=DEVELOPMENT.2026-10-04T00-00-00Z", result.stdout)

    def test_unknown_is_explicit_and_does_not_panic(self):
        result = self.run_generator("unknown")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("cmd.CommitID=unknown", result.stdout)
        self.assertIn("cmd.ShortCommitID=unknown", result.stdout)

    def test_malformed_metadata_is_rejected(self):
        for value in ("abc", "A" * 40, "z" * 40, "a" * 39, "a" * 41, "a\nsha=bad"):
            with self.subTest(value=value):
                result = self.run_generator(value)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("OTTERIO_BUILD_COMMIT must be", result.stderr)

    def test_gitless_build_requires_explicit_override(self):
        result = self.run_generator(None)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Error generating git commit-id", result.stderr)


if __name__ == "__main__":
    unittest.main()
