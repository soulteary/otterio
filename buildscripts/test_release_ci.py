"""Exercise the release CI gate offline, including tag/main push races."""

from contextlib import redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("release_ci", ROOT / "buildscripts/release-ci.py")
ci = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ci)
REPOSITORY = "soulteary/otterio"
SHA = "a" * 40
URL = "https://github.com/soulteary/otterio/actions/runs/123"


def run(status="completed", conclusion="success"):
    return dict(head_sha=SHA, head_branch="main", event="push", status=status,
                conclusion=conclusion, html_url=URL)


class Clock:
    def __init__(self):
        self.now = 0
        self.sleeps = []

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.sleeps.append(seconds)
        self.now += seconds


class WaitTests(unittest.TestCase):
    def setUp(self):
        self.clock = Clock()
        self.output = io.StringIO()
        self.addCleanup(patch.stopall)
        patch.object(ci.time, "monotonic", self.clock.monotonic).start()
        patch.object(ci.time, "sleep", self.clock.sleep).start()

    def wait(self, rounds, timeout=90, interval=30):
        responses = [item for row in rounds for item in row]
        with patch.object(ci, "fetch_run", side_effect=responses) as fetch:
            with redirect_stdout(self.output):
                ci.wait_for_ci(REPOSITORY, SHA, timeout=timeout, interval=interval)
        return fetch

    def test_all_three_main_push_checks_must_succeed(self):
        fetch = self.wait([[run(), run(), run()]])
        self.assertEqual([call.args for call in fetch.call_args_list],
                         [(REPOSITORY, SHA, name) for name in
                          ("go.yml", "lint.yml", "release-checks.yml")])
        self.assertEqual(self.clock.sleeps, [])

    def test_tag_can_arrive_before_go_ci_finishes(self):
        fetch = self.wait([
            [run("queued", None), run(), run()],
            [run("in_progress", None), run(), run()],
            [run(), run(), run()],
        ])
        self.assertEqual(fetch.call_count, 9)
        self.assertEqual(self.clock.sleeps, [30, 30])
        self.assertIn("go.yml", self.output.getvalue())
        self.assertIn(URL, self.output.getvalue())

    def test_run_can_appear_after_tag_event(self):
        self.wait([[None, run(), run()], [run(), run(), run()]])
        self.assertEqual(self.clock.sleeps, [30])
        self.assertIn("go.yml", self.output.getvalue())

    def test_scheduled_and_approval_waiting_statuses_remain_blocked(self):
        for status in ("requested", "waiting", "pending"):
            with self.subTest(status=status):
                self.wait([[run(status, None), run(), run()], [run(), run(), run()]])
        self.assertEqual(self.clock.sleeps, [30, 30, 30])

    def test_each_workflow_can_block_release(self):
        for position in range(3):
            with self.subTest(position=position):
                row = [run(), run(), run()]
                row[position] = run("in_progress", None)
                self.wait([row, [run(), run(), run()]])
        self.assertEqual(self.clock.sleeps, [30, 30, 30])

    def test_non_success_conclusions_are_never_retried_or_accepted(self):
        for conclusion in ("failure", "cancelled", "timed_out", "skipped",
                           "neutral", "stale", "action_required", None):
            with self.subTest(conclusion=conclusion):
                with self.assertRaises(ci.CIError) as error:
                    self.wait([[run(conclusion=conclusion), run(), run()]])
                self.assertIn("go.yml", str(error.exception))
                self.assertIn(URL, str(error.exception))
        self.assertEqual(self.clock.sleeps, [])

    def test_later_failure_stops_while_go_is_still_pending(self):
        with self.assertRaises(ci.CIError) as error:
            self.wait([[run("in_progress", None), run(conclusion="failure"), run()]])
        self.assertIn("lint.yml", str(error.exception))
        self.assertEqual(self.clock.sleeps, [])

    def test_success_is_rechecked_during_wait(self):
        with self.assertRaises(ci.CIError):
            self.wait([[run(), run("in_progress", None), run()],
                       [run(conclusion="cancelled"), run(), run()]])
        self.assertEqual(self.clock.sleeps, [30])

    def test_missing_or_running_checks_timeout_without_oversleeping(self):
        for pending in (None, run("in_progress", None)):
            with self.subTest(pending=pending):
                self.clock.now = 0
                self.clock.sleeps.clear()
                with self.assertRaises(ci.CIError) as error:
                    self.wait([[pending, run(), run()]] * 3, timeout=35)
                self.assertIn("go.yml", str(error.exception))
                self.assertEqual(self.clock.sleeps, [30, 5])

    def test_unknown_status_fails_closed(self):
        with self.assertRaises(ci.CIError):
            self.wait([[run("unexpected", None), run(), run()]])
        self.assertEqual(self.clock.sleeps, [])

    def test_no_api_requests_after_wait_deadline(self):
        with patch.object(ci, "fetch_run", side_effect=[None, run(), run()]) as fetch:
            with redirect_stdout(self.output), self.assertRaises(ci.CIError):
                ci.wait_for_ci(REPOSITORY, SHA, timeout=30, interval=30)
        self.assertEqual(fetch.call_count, 3)
        self.assertEqual(self.clock.sleeps, [30])

    def test_slow_api_cannot_pass_after_deadline(self):
        def slow_fetch(*args):
            self.clock.now += 15
            return run()
        with patch.object(ci, "fetch_run", side_effect=slow_fetch):
            with redirect_stdout(self.output), self.assertRaises(ci.CIError):
                ci.wait_for_ci(REPOSITORY, SHA, timeout=30, interval=30)
        self.assertEqual(self.clock.sleeps, [])


class APITests(unittest.TestCase):
    def fetch(self, payload):
        response = subprocess.CompletedProcess([], 0, json.dumps(payload), "")
        with patch.object(ci.subprocess, "run", return_value=response) as command:
            result = ci.fetch_run(REPOSITORY, SHA, "go.yml")
        return result, command

    def test_request_filters_exact_sha_main_and_push(self):
        result, command = self.fetch({"workflow_runs": [run()]})
        self.assertEqual(result, run())
        args = command.call_args.args[0]
        self.assertEqual(args[:2], ["gh", "api"])
        endpoint = next(arg for arg in args if arg.startswith("repos/"))
        parsed = urlsplit(endpoint)
        self.assertEqual(parsed.path, "repos/soulteary/otterio/actions/workflows/go.yml/runs")
        filters = parse_qs(parsed.query)
        if "-f" in args:
            self.assertEqual(args[args.index("--method") + 1], "GET")
            for index, arg in enumerate(args):
                if arg == "-f":
                    key, value = args[index + 1].split("=", 1)
                    filters[key] = [value]
        self.assertEqual(filters,
                         dict(head_sha=[SHA], branch=["main"], event=["push"], per_page=["1"]))
        self.assertEqual(command.call_args.kwargs["timeout"], 30)
        self.assertFalse(command.call_args.kwargs.get("shell", False))

    def test_no_run_is_pending(self):
        result, _ = self.fetch({"workflow_runs": []})
        self.assertIsNone(result)

    def test_api_cannot_substitute_different_source_or_event(self):
        for key, wrong in (("head_sha", "b" * 40), ("head_branch", "topic"),
                           ("event", "pull_request"), ("event", "workflow_dispatch")):
            with self.subTest(key=key, wrong=wrong):
                value = dict(run(), **{key: wrong})
                with self.assertRaises(ci.CIError):
                    self.fetch({"workflow_runs": [value]})

    def test_malformed_response_is_not_missing_ci(self):
        for payload in ({}, [], {"workflow_runs": None}, {"workflow_runs": {}},
                        {"workflow_runs": [None]}, {"workflow_runs": [{}]},
                        {"workflow_runs": [run(), run()]}):
            with self.subTest(payload=payload), self.assertRaises(ci.CIError):
                self.fetch(payload)

    def test_api_failure_and_invalid_json_fail_closed(self):
        responses = [subprocess.CompletedProcess([], 1, "", "authentication failed"),
                     subprocess.CompletedProcess([], 0, "not json", "")]
        for response in responses:
            with self.subTest(response=response):
                with patch.object(ci.subprocess, "run", return_value=response):
                    with self.assertRaises(ci.CIError):
                        ci.fetch_run(REPOSITORY, SHA, "go.yml")

    def test_request_timeout_and_missing_gh_fail_closed(self):
        for error in (subprocess.TimeoutExpired("gh", 30), FileNotFoundError("gh")):
            with self.subTest(error=error):
                with patch.object(ci.subprocess, "run", side_effect=error):
                    with self.assertRaises(ci.CIError):
                        ci.fetch_run(REPOSITORY, SHA, "go.yml")


class WorkflowTests(unittest.TestCase):
    def test_bad_cli_input_fails_before_api_request(self):
        cases = [[], ["../otterio", SHA], [REPOSITORY, "bad-sha"],
                 [REPOSITORY, SHA, "--timeout", "0"],
                 [REPOSITORY, SHA, "--interval", "-1"],
                 [REPOSITORY, SHA, "--timeout", "nan"]]
        for args in cases:
            with self.subTest(args=args):
                result = subprocess.run([sys.executable, str(ROOT / "buildscripts/release-ci.py"), *args],
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("::error::", result.stderr)

    def test_checked_in_gate_executes_helper_and_preserves_failure(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        gate = workflow.split("      - name: Require successful main CI for the exact commit\n", 1)[1]
        gate = gate.split("      - name:", 1)[0]
        self.assertIn("timeout-minutes: 65", gate)
        self.assertIn("GH_TOKEN: ${{ github.token }}", gate)
        self.assertIn("RELEASE_SHA: ${{ steps.meta.outputs.sha }}", gate)
        script = gate.split("        run: ", 1)[1].strip()
        self.assertIn("--timeout 3600 --interval 30", script)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stub = root / "gh"
            stub.write_text("#!" + sys.executable + "\nimport os\nprint(os.environ['CI_RESPONSE'])\n")
            stub.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                       GITHUB_REPOSITORY=REPOSITORY, RELEASE_SHA=SHA, GH_TOKEN="test-only",
                       PYTHONDONTWRITEBYTECODE="1")
            for conclusion, code in (("success", 0), ("failure", 1), ("cancelled", 1)):
                env["CI_RESPONSE"] = json.dumps({"workflow_runs": [run(conclusion=conclusion)]})
                result = subprocess.run(["bash", "-c", script], cwd=ROOT, env=env,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, code, result.stdout + result.stderr)
                if code:
                    self.assertIn("::error::", result.stderr)


if __name__ == "__main__":
    unittest.main()
