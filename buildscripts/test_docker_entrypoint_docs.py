"""Execute documented shell examples with a recording Docker stub, never a daemon."""
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
USER = "docs-test-admin"
PASSWORD = "docs-test-password-not-for-deployment"


def shell_blocks(text):
    return re.findall(r"^```sh\n(.*?)^```", text, re.MULTILINE | re.DOTALL)


class DockerDocumentationTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.temp = Path(self.directory.name)
        self.record = self.temp / "docker-call.json"
        docker = self.temp / "docker"
        docker.write_text(
            "#!" + sys.executable + "\n"
            "import json, os, pathlib, sys\n"
            "pathlib.Path(os.environ['DOC_TEST_RECORD']).write_text(json.dumps({"
            "'args': sys.argv[1:], 'user': os.environ.get('OTTERIO_ROOT_USER'), "
            "'password': os.environ.get('OTTERIO_ROOT_PASSWORD')}))\n",
            encoding="utf-8",
        )
        docker.chmod(0o755)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("OTTERIO_")}
        self.env.update(PATH=str(self.temp) + os.pathsep + os.environ.get("PATH", ""),
                        DOC_TEST_RECORD=str(self.record))
        text = (ROOT / "docs/erasure/README.md").read_text(encoding="utf-8")
        blocks = [block for block in shell_blocks(text) if "docker run " in block]
        self.assertEqual(len(blocks), 1, "expected one executable erasure Docker example")
        self.example = blocks[0]

    def execute(self, credentials=None, shell="sh"):
        self.record.unlink(missing_ok=True)
        return subprocess.run([shell, "-eu", "-c", self.example], cwd=self.temp,
                              env=dict(self.env, **(credentials or {})),
                              text=True, capture_output=True, timeout=10)

    def configured_call(self, shell="sh"):
        result = self.execute({"OTTERIO_ROOT_USER": USER, "OTTERIO_ROOT_PASSWORD": PASSWORD}, shell)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(self.record.read_text(encoding="utf-8"))

    def test_erasure_passes_configured_credentials(self):
        call = self.configured_call()
        args = call["args"]
        variables = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "-e"]
        self.assertIn("OTTERIO_ROOT_USER", variables)
        self.assertIn("OTTERIO_ROOT_PASSWORD", variables)
        self.assertEqual(call["user"], USER)
        self.assertEqual(call["password"], PASSWORD)
        self.assertNotIn("OTTERIO_ALLOW_DEFAULT_CREDENTIALS", self.example)

    def test_erasure_requires_both_credentials_before_docker(self):
        for credentials in ({}, {"OTTERIO_ROOT_USER": USER},
                            {"OTTERIO_ROOT_PASSWORD": PASSWORD},
                            {"OTTERIO_ROOT_USER": USER, "OTTERIO_ROOT_PASSWORD": ""}):
            with self.subTest(credentials=list(credentials)):
                result = self.execute(credentials)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.record.exists(), "Docker must not be called without credentials")
                self.assertNotIn(PASSWORD, result.stdout + result.stderr)

    def test_erasure_keeps_eight_volumes_and_literal_range(self):
        for shell in ("sh", "bash"):
            with self.subTest(shell=shell):
                args = self.configured_call(shell)["args"]
                volumes = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "-v"]
                self.assertEqual(volumes, [f"/mnt/data{i}:/data{i}" for i in range(1, 9)])
                self.assertEqual(args[-2:], ["server", "/data{1...8}"])

    def test_erasure_publishes_only_to_loopback(self):
        args = self.configured_call()["args"]
        ports = [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "-p"]
        self.assertEqual(ports, ["127.0.0.1:9000:9000"])

    def test_console_certificate_summary_names_distinct_flags(self):
        text = (ROOT / "README.md").read_text(encoding="utf-8")
        summary = next(line for line in text.splitlines() if line.startswith("- The S3 listener always uses"))
        self.assertEqual(re.findall(r"`([^`]+)`", summary), ["--certs-dir", "--console-certs-dir"])

    def test_console_tls_example_uses_separate_paths(self):
        text = (ROOT / "README.md").read_text(encoding="utf-8")
        block = next(block for block in shell_blocks(text) if "--console-certs-dir /" in block)
        args = shlex.split(block.replace("\\\n", " "))
        self.assertEqual(args[args.index("--certs-dir") + 1], "/etc/otterio/certs/s3")
        self.assertEqual(args[args.index("--console-certs-dir") + 1], "/etc/otterio/certs/console")
        self.assertLess(args.index("--console-certs-dir"), args.index("/data"))

    def test_ci_watches_the_linked_erasure_guide(self):
        workflow = (ROOT / ".github/workflows/container-security-checks.yml").read_text(encoding="utf-8")
        paths = [line for line in workflow.splitlines() if line.strip().startswith("paths:")]
        self.assertEqual(len(paths), 2)
        for line in paths:
            self.assertIn("'docs/erasure/README.md'", line)
            self.assertIn("'buildscripts/test_docker_entrypoint*.py'", line)
        self.assertIn("-p 'test_docker_entrypoint*.py'", workflow)


if __name__ == "__main__":
    unittest.main()
