"""Keep the cached runtime layer independent and its security refresh explicit."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import textwrap
import unittest

ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github/workflows"


class RuntimeCacheTests(unittest.TestCase):
    def setUp(self):
        self.edge = (WORKFLOWS / "docker.yml").read_text(encoding="utf-8")
        self.release = (WORKFLOWS / "release.yml").read_text(encoding="utf-8")

    def test_only_refresh_arg_precedes_package_installation(self):
        text = (ROOT / "Dockerfile.ci").read_text(encoding="utf-8")
        instructions = [line.strip() for line in text.replace("\\\n", " ").splitlines()
                        if line.strip() and not line.lstrip().startswith("#")]
        install = next(i for i, line in enumerate(instructions)
                       if line.startswith("RUN ") and "microdnf update" in line)
        # Docker includes preceding ARG values in RUN's environment/cache key.
        # Even an unused RELEASE declaration here would defeat binary reuse.
        args = [line.split()[1].split("=", 1)[0] for line in instructions[:install]
                if line.startswith("ARG ")]
        self.assertEqual(args, ["RUNTIME_REFRESH"])
        self.assertFalse(any(line.startswith(("COPY ", "LABEL ", "ENV "))
                             for line in instructions[:install]))
        self.assertIn("microdnf install curl ca-certificates shadow-utils util-linux", instructions[install])
        self.assertIn("microdnf clean all", instructions[install])
        binary = next(i for i, line in enumerate(instructions)
                      if line.startswith("COPY dist/otterio-linux-"))
        self.assertLess(install, instructions.index("ARG RELEASE"))
        self.assertLess(install, binary)
        self.assertTrue(any(line.startswith("RUN chmod ") for line in instructions[binary + 1:]))

    def runtime_key(self, day, manual=False, attempt="1"):
        step = re.search(r"(?ms)^      - name: Select runtime dependency refresh\n"
                         r"(.*?)(?=^      - |\Z)", self.edge)[1]
        script = textwrap.dedent(step.split("        run: |\n", 1)[1])
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            fake_date = directory / "date"
            fake_date.write_text('#!/bin/sh\n[ "$*" = "-u +%Y-%m-%d" ] || exit 2\n'
                                 + f"printf '%s\\n' '{day}'\n", encoding="utf-8")
            fake_date.chmod(0o755)
            output = directory / "output"
            env = dict(os.environ, PATH=str(directory) + os.pathsep + os.environ["PATH"],
                       FORCE_REFRESH="true" if manual else "false", GITHUB_RUN_ID="1234",
                       GITHUB_RUN_ATTEMPT=attempt, GITHUB_OUTPUT=str(output))
            result = subprocess.run(["bash", "-c", script], env=env, capture_output=True,
                                    text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            return output.read_text(encoding="utf-8").strip().removeprefix("key=")

    def test_edge_refreshes_each_utc_day(self):
        self.assertEqual(self.runtime_key("2026-10-08"), "2026-10-08")
        self.assertEqual(self.runtime_key("2026-10-09"), "2026-10-09")
        self.assertIn("FORCE_REFRESH: ${{ github.event_name == 'workflow_dispatch' }}", self.edge)
        self.assertIn("RUNTIME_REFRESH=${{ steps.runtime.outputs.key }}", self.edge)

    def test_manual_edge_run_and_rerun_force_fresh_packages(self):
        self.assertEqual(self.runtime_key("2026-10-08", manual=True), "2026-10-08-1234-1")
        self.assertEqual(self.runtime_key("2026-10-08", manual=True, attempt="2"), "2026-10-08-1234-2")

    def test_every_stable_release_attempt_refreshes_packages(self):
        self.assertIn("RUNTIME_REFRESH=release-${{ github.run_id }}-${{ github.run_attempt }}", self.release)

    def test_edge_and_release_use_separate_persistent_scopes(self):
        scopes = []
        for workflow in (self.edge, self.release):
            source = re.search(r"cache-from: type=gha,scope=([\w-]+)", workflow)[1]
            target = re.search(r"cache-to: type=gha,scope=([\w-]+),mode=max", workflow)[1]
            self.assertEqual(source, target)
            scopes.append(source)
        self.assertNotEqual(*scopes)


if __name__ == "__main__":
    unittest.main()
