"""Regression tests for the race launcher, Makefile wiring and diagnostics."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("summarize_race", ROOT / "buildscripts/summarize_race.py")
SUMMARY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SUMMARY)


@unittest.skipUnless(os.name == "posix" and shutil.which("bash"), "POSIX race launcher")
class RaceLauncherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        (self.root / "buildscripts").mkdir()
        shutil.copy(ROOT / "buildscripts/race.sh", self.root / "buildscripts/race.sh")
        shutil.copy(ROOT / "Makefile", self.root / "Makefile")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.log = self.root / "calls.jsonl"
        go = self.bin / "go"
        go.write_text('''#!/usr/bin/env python3
import json, os, sys
with open(os.environ["FAKE_GO_LOG"], "a", encoding="utf-8") as stream:
    stream.write(json.dumps({"args": sys.argv[1:], "cgo": os.environ.get("CGO_ENABLED"), "cwd": os.getcwd()}) + "\\n")
if sys.argv[1:3] == ["list", "-m"]:
    print(os.environ.get("FAKE_MODULE", "example.test/storage"))
    sys.exit(int(os.environ.get("FAKE_MODULE_EXIT", "0")))
if sys.argv[1] == "list":
    print(os.environ.get("FAKE_PACKAGES", "example.test/storage\\nexample.test/storage/cmd\\nexample.test/storage/browser\\nexample.test/storage/browser/assets\\nexample.test/storage/pkg/browsercache"))
    sys.exit(int(os.environ.get("FAKE_LIST_EXIT", "0")))
if sys.argv[1] == "test":
    sys.exit(int(os.environ.get("FAKE_TEST_EXIT", "0")))
if sys.argv[1] == "env":
    print("/tmp/fake-go")
    sys.exit(0)
sys.exit(91)
''', encoding="utf-8")
        go.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        FAKE_GO_LOG=str(self.log), CGO_ENABLED="0")

    def run_script(self, *args, **env):
        return subprocess.run(["bash", str(self.root / "buildscripts/race.sh"), *args],
                              cwd=self.bin, env=dict(self.env, **env),
                              capture_output=True, text=True, timeout=10)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def test_full_selection_and_matching_build_flags(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        self.assertEqual(calls[1]["args"], ["list", "-tags", "kqueue", "-race", "./..."])
        self.assertEqual(calls[2]["args"], ["test", "-tags", "kqueue", "-race", "-timeout", "20m", "-count=1",
                                         "example.test/storage", "example.test/storage/cmd", "example.test/storage/pkg/browsercache"])
        self.assertTrue(all(call["cgo"] == "1" for call in calls))
        self.assertTrue(all(call["cwd"] == str(self.root) for call in calls))

    def test_partial_enumeration_failure_never_runs_tests(self):
        result = self.run_script(FAKE_LIST_EXIT="42", FAKE_PACKAGES="example.test/storage/cmd")
        self.assertEqual(result.returncode, 42)
        self.assertFalse(any(call["args"][0] == "test" for call in self.calls()))

    def test_module_failure_never_runs_tests(self):
        self.assertEqual(self.run_script(FAKE_MODULE_EXIT="43").returncode, 43)
        self.assertEqual(len(self.calls()), 1)

    def test_empty_or_browser_only_selection_is_an_error(self):
        for packages in ("", "example.test/storage/browser\nexample.test/storage/browser/assets"):
            with self.subTest(packages=packages):
                self.assertNotEqual(self.run_script(FAKE_PACKAGES=packages).returncode, 0)
        self.assertFalse(any(call["args"][0] == "test" for call in self.calls()))

    def test_invalid_module_is_an_error(self):
        for module in ("", "example.test/one\nexample.test/two"):
            with self.subTest(module=module):
                self.assertNotEqual(self.run_script(FAKE_MODULE=module).returncode, 0)

    def test_test_failure_is_propagated(self):
        self.assertEqual(self.run_script(FAKE_TEST_EXIT="66").returncode, 66)

    def test_json_option_is_passed_to_go(self):
        self.assertEqual(self.run_script("-json").returncode, 0)
        self.assertIn("-json", self.calls()[-1]["args"])

    @unittest.skipUnless(shutil.which("make"), "make required")
    def test_make_target_does_not_build_or_generate_ldflags(self):
        # Even a file named test-race must not suppress the phony target.
        (self.root / "test-race").touch()
        result = subprocess.run(["make", "test-race"], cwd=self.root, env=self.env,
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = [call["args"][0] for call in self.calls()]
        self.assertIn("test", commands)
        self.assertNotIn("build", commands)
        self.assertNotIn("run", commands)

    def test_tee_pipeline_keeps_failure_status(self):
        result = subprocess.run(["bash", "-c", 'set -euo pipefail; bash "$1" -json | tee "$2"',
                                 "race-ci", str(self.root / "buildscripts/race.sh"), str(self.root / "race.jsonl")],
                                env=dict(self.env, FAKE_TEST_EXIT="66"), capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 66)


class RaceSummaryTests(unittest.TestCase):
    def render(self, events):
        return SUMMARY.summarize(json.dumps(event) + "\n" for event in events)

    def test_subtests_do_not_double_count(self):
        report = self.render([
            {"Action": "pass", "Package": "p", "Test": "TestX/sub", "Elapsed": 4},
            {"Action": "pass", "Package": "p", "Test": "TestX", "Elapsed": 5},
            {"Action": "pass", "Package": "p", "Elapsed": 6},
        ])
        self.assertIn("Top-level tests: 1 passed", report)
        self.assertNotIn("TestX/sub", report)
        self.assertIn("5.000", report)

    def test_failures_skips_and_fuzz_seeds_are_visible(self):
        report = self.render([
            {"Action": "fail", "Package": "p", "Test": "TestFail", "Elapsed": 3},
            {"Action": "skip", "Package": "p", "Test": "TestSkip"},
            {"Action": "pass", "Package": "p", "Test": "FuzzX", "Elapsed": 1},
            {"Action": "fail", "Package": "p", "Elapsed": 5},
        ])
        self.assertIn("Top-level tests: 1 passed, 1 failed, 1 skipped", report)
        self.assertIn("Packages: 0 passed, 1 failed", report)
        self.assertLess(report.index("p / TestFail"), report.index("p / FuzzX"))

    def test_truncated_stream_is_not_reported_as_success(self):
        report = self.render([{"Action": "start", "Package": "p"}])
        self.assertIn("Incomplete package results: 1", report)
        self.assertIn("not evidence of a passing suite", report)

    def test_invalid_json_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "line 1"):
            SUMMARY.summarize(["not json"])

    def test_empty_stream(self):
        self.assertIn("No completed package results", self.render([]))


if __name__ == "__main__":
    unittest.main()
