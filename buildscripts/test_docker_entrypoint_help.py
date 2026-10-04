"""Help/endpoint classification regressions against the real entrypoint."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ENTRYPOINT = Path(__file__).resolve().parents[1] / "dockerscripts/docker-entrypoint.sh"


class EntrypointHelpTests(unittest.TestCase):
    def probe(self, args, values=None):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "otterio"
            # Deliberately exit 1, like serverCmdArgs' implicit help path.
            # Reaching the binary, not exit==0, proves help was not intercepted.
            binary.write_text('#!/bin/sh\nprintf "CLI reached\\n"\nprintf "<%s>\\n" "$@"\nexit 1\n')
            binary.chmod(0o755)
            env = {k: v for k, v in os.environ.items() if not k.startswith("OTTERIO_")}
            env["PATH"] = directory + os.pathsep + env.get("PATH", "")
            env.update(values or {})
            return subprocess.run(
                ["sh", str(ENTRYPOINT), *args], cwd=directory, env=env,
                text=True, capture_output=True, timeout=10,
            )

    def assert_help(self, args, values=None):
        result = self.probe(args, values)
        self.assertEqual(result.returncode, 1)
        self.assertIn("CLI reached", result.stdout)
        self.assertNotIn("configure OTTERIO_ROOT_USER", result.stderr)
        self.assertNotIn("WARNING", result.stderr)

    def assert_gated(self, args, values=None):
        result = self.probe(args, values)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("CLI reached", result.stdout)
        self.assertIn("configure OTTERIO_ROOT_USER", result.stderr)

    def test_positional_help_and_empty_server(self):
        for args in (("server",), ("server", "help"), ("otterio", "server", "help")):
            with self.subTest(args=args):
                self.assert_help(args)

    def test_flags_without_endpoints(self):
        for args in (
            ("server", "--address", ":9000"),
            ("--quiet", "server"),
            ("server", "--address=:9000", "help"),
            ("server", "--console-certs-dir", "/certs", "help"),
            ("server", "--certs-dir", "help"),
        ):
            with self.subTest(args=args):
                self.assert_help(args)

    def test_environment_endpoints_remain_gated(self):
        for name in ("OTTERIO_ARGS", "OTTERIO_ENDPOINTS"):
            for args in (("server",), ("server", "help"), ("server", "--address", ":9000")):
                with self.subTest(name=name, args=args):
                    self.assert_gated(args, {name: "/data"})

    def test_empty_environment_endpoints_allow_help(self):
        self.assert_help(("server",), {"OTTERIO_ARGS": "", "OTTERIO_ENDPOINTS": ""})

    def test_explicit_help_and_version(self):
        for args in (
            (), ("--help",), ("--version",), ("server", "-h"),
            ("server", "--help"), ("gateway",), ("gateway", "s3", "--help"),
        ):
            with self.subTest(args=args):
                self.assert_help(args, {"OTTERIO_ARGS": "/data"})

    def test_help_like_flag_values_do_not_bypass_startup(self):
        for value in ("help", "--help", "-h", "--version"):
            with self.subTest(value=value):
                self.assert_gated(("server", "--certs-dir", value, "/data"))
                self.assert_gated(("--certs-dir", value, "server", "/data"))

    def test_data_paths_do_not_bypass_startup(self):
        for args in (
            ("server", "/data"), ("server", "./help"), ("server", "/data", "help"),
            ("server", "/data", "--help"), ("server", "--", "--help"),
            ("--", "server", "/data"), ("gateway", "nas", "/data"),
            ("gateway", "--", "nas", "/data"),
        ):
            with self.subTest(args=args):
                self.assert_gated(args)

    def test_end_of_options_help(self):
        self.assert_help(("server", "--"))
        self.assert_help(("server", "--", "help"))
        self.assert_help(("--", "server", "--help"))

    def test_valid_credentials_preserve_arguments(self):
        args = ("server", "--address", ":9000", "/a path", "help")
        result = self.probe(args, {"OTTERIO_ROOT_USER": "test-admin", "OTTERIO_ROOT_PASSWORD": "test-password"})
        self.assertIn("CLI reached", result.stdout)
        self.assertEqual(result.stdout.splitlines()[1:], ["<" + arg + ">" for arg in args])


@unittest.skipUnless(os.environ.get("OTTERIO_TEST_BINARY"), "set OTTERIO_TEST_BINARY to test the real CLI")
class RealCLIHelpTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "otterio").symlink_to(Path(os.environ["OTTERIO_TEST_BINARY"]).resolve())
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("OTTERIO_")}
        self.env.update(PATH=str(self.root) + os.pathsep + self.env.get("PATH", ""), HOME=str(self.root))

    def run_cli(self, args, values=None):
        return subprocess.run(
            ["sh", str(ENTRYPOINT), *args], cwd=self.root,
            env=dict(self.env, **(values or {})), text=True, capture_output=True, timeout=10,
        )

    def test_real_help_output_including_exit_one(self):
        for args in (("server",), ("server", "help"), ("server", "--help"), ("--help",),
                     ("server", "--address", "127.0.0.1:0")):
            with self.subTest(args=args):
                result = self.run_cli(args)
                self.assertIn(result.returncode, (0, 1))
                self.assertIn("USAGE:", result.stdout + result.stderr)
                self.assertNotIn("OtterIO entrypoint:", result.stdout + result.stderr)
        result = self.run_cli(("--version",))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("version", (result.stdout + result.stderr).lower())

    def test_environment_endpoints_cannot_start_without_credentials(self):
        for name in ("OTTERIO_ARGS", "OTTERIO_ENDPOINTS"):
            for args in (("server", "--address", "127.0.0.1:0"),
                         ("server", "--address", "127.0.0.1:0", "help")):
                with self.subTest(name=name, args=args):
                    result = self.run_cli(args, {name: str(self.root / "data")})
                    self.assertEqual(result.returncode, 1)
                    self.assertIn("configure OTTERIO_ROOT_USER", result.stderr)


if __name__ == "__main__":
    unittest.main()
