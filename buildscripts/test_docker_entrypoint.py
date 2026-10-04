"""Exercise the real POSIX entrypoint with a fake binary; never start a server."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ENTRYPOINT = Path(__file__).resolve().parents[1] / "dockerscripts/docker-entrypoint.sh"


class EntrypointTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        binary = self.root / "otterio"
        binary.write_text(
            "#!/usr/bin/env python3\nimport json, os, sys\n"
            "print(json.dumps({'args': sys.argv[1:], 'env': {k: v for k, v in os.environ.items() if k.startswith('OTTERIO_')}}))\n"
        )
        binary.chmod(0o755)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("OTTERIO_")}
        self.env["PATH"] = str(self.root) + os.pathsep + self.env.get("PATH", "")
        for var, name in (
            ("ACCESS_KEY", "access_key"), ("SECRET_KEY", "secret_key"),
            ("ROOT_USER", "access_key"), ("ROOT_PASSWORD", "secret_key"),
            ("KMS_MASTER_KEY", "kms_master_key"), ("SSE_MASTER_KEY", "sse_master_key"),
        ):
            self.env["OTTERIO_" + var + "_FILE"] = name

    def run_entrypoint(self, values=None, args=("server", "/data")):
        return subprocess.run(
            ["sh", str(ENTRYPOINT), *args], cwd=self.root,
            env=dict(self.env, **(values or {})), text=True, capture_output=True, timeout=10,
        )

    def secret(self, name="password", content="a-test-only-secret\n"):
        path = self.root / name
        path.write_text(content)
        return str(path)

    def successful_env(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)["env"]

    def test_environment_pair(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_USER": "tester", "OTTERIO_ROOT_PASSWORD": "test-only-password"})
        self.successful_env(result)
        self.assertEqual(json.loads(result.stdout)["args"], ["server", "/data"])

    def test_username_env_password_file(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_USER": "tester", "OTTERIO_ROOT_PASSWORD_FILE": self.secret()})
        self.assertEqual(self.successful_env(result)["OTTERIO_ROOT_PASSWORD"], "a-test-only-secret")

    def test_username_file_password_env(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_USER_FILE": self.secret("user", "tester"), "OTTERIO_ROOT_PASSWORD": "test-only-password"})
        self.assertEqual(self.successful_env(result)["OTTERIO_ROOT_USER"], "tester")

    def test_legacy_pair(self):
        result = self.run_entrypoint({"OTTERIO_ACCESS_KEY": "tester", "OTTERIO_SECRET_KEY": "test-only-password"})
        self.successful_env(result)

    def test_missing_explicit_file(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_PASSWORD_FILE": str(self.root / "missing")})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("readable regular file", result.stderr)

    def test_empty_file(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_PASSWORD_FILE": self.secret(content="\n")})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not be empty", result.stderr)

    def test_directory_is_not_a_secret(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_PASSWORD_FILE": str(self.root)})
        self.assertNotEqual(result.returncode, 0)

    @unittest.skipIf(os.geteuid() == 0, "root can read mode-000 files")
    def test_unreadable_file(self):
        path = Path(self.secret())
        path.chmod(0)
        result = self.run_entrypoint({"OTTERIO_ROOT_PASSWORD_FILE": str(path)})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("readable regular file", result.stderr)

    def test_conflicting_sources_fail_without_leaking_secret(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_PASSWORD": "do-not-log-me", "OTTERIO_ROOT_PASSWORD_FILE": self.secret()})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not both", result.stderr)
        self.assertNotIn("do-not-log-me", result.stderr + result.stdout)

    def test_secret_values_are_not_evaluated(self):
        marker = self.root / "must-not-exist"
        value = '$(touch "' + str(marker) + '"); * $HOME'
        result = self.run_entrypoint({"OTTERIO_ROOT_USER": "tester", "OTTERIO_ROOT_PASSWORD_FILE": self.secret(content=value)})
        self.assertEqual(self.successful_env(result)["OTTERIO_ROOT_PASSWORD"], value)
        self.assertFalse(marker.exists())

    def test_missing_credentials_are_rejected(self):
        result = self.run_entrypoint()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("configure OTTERIO_ROOT_USER", result.stderr)

    def test_default_credentials_are_rejected(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_USER": "otterioadmin", "OTTERIO_ROOT_PASSWORD": "otterioadmin"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("default credentials", result.stderr)

    def test_development_opt_in(self):
        result = self.run_entrypoint({"OTTERIO_ALLOW_DEFAULT_CREDENTIALS": "1"})
        self.successful_env(result)
        self.assertIn("WARNING", result.stderr)

    def test_partial_pair_is_never_allowed(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_USER": "tester", "OTTERIO_ALLOW_DEFAULT_CREDENTIALS": "1"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("both username and password", result.stderr)

    def test_modern_pair_does_not_borrow_legacy_password(self):
        result = self.run_entrypoint({"OTTERIO_ROOT_USER": "tester", "OTTERIO_ACCESS_KEY": "olduser", "OTTERIO_SECRET_KEY": "old-password"})
        self.assertNotEqual(result.returncode, 0)

    def test_help_and_version_need_no_credentials(self):
        for args in (("--version",), ("--help",), ("server", "--help")):
            with self.subTest(args=args):
                self.successful_env(self.run_entrypoint(args=args))

    def test_partial_user_switch_fails(self):
        result = self.run_entrypoint({"OTTERIO_USERNAME": "nobody"}, args=("--version",))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must be set together", result.stderr)


if __name__ == "__main__":
    unittest.main()
