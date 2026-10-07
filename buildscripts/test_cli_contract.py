"""Runner unit tests plus an opt-in real candidate/baseline comparison."""
import json
import hashlib
import os
from pathlib import Path, PureWindowsPath
import subprocess
import sys
import tempfile
import unittest

import cli_contract


class CLIContractRunnerTests(unittest.TestCase):
    def test_commands_are_read_only_from_the_visible_commands_section(self):
        help_text = "NAME:\n  fake\nCOMMANDS:\n  list, ls  list objects\n  help, h  help\n  admin     administration\nFLAGS:\n  accidental  not a command\n"
        self.assertEqual(cli_contract.command_names(help_text), ["list", "admin"])

    def test_normalization_preserves_output_and_replaces_only_controlled_root(self):
        value = " error -flag=/tmp/fixture/certs\n  :9000 version-invalid\n"
        self.assertEqual(cli_contract.normalize(value, Path("/tmp/fixture")),
                         " error -flag={sandbox}/certs\n  :9000 version-invalid\n")

    def test_quoted_windows_home_default_uses_controlled_path_only(self):
        root = PureWindowsPath(r"C:\cli-fixture")
        text = 'default: ' + json.dumps(str(root / "home" / ".oc")) + ' other: C:\\unchanged\\certs'
        self.assertEqual(cli_contract.normalize(text, root),
                         'default: "{sandbox}/home/.oc" other: C:\\unchanged\\certs')

    def test_stream_exit_and_side_effect_differences_are_not_accepted(self):
        before = {"platform_family": "unix", "cases": [{"id": "x", "exit_code": 1,
                   "stdout": "", "stderr": "error\n", "side_effects": []}]}
        for changed_field, changed_value in [("exit_code", 0), ("stdout", "error\n"),
                                              ("stderr", ""), ("side_effects", [{"path": "config"}])]:
            after = json.loads(json.dumps(before))
            after["cases"][0][changed_field] = changed_value
            self.assertTrue(cli_contract.differences(before, after))

    def test_added_or_removed_command_is_reported(self):
        before = {"platform_family": "unix", "cases": [{"id": "help/old"}]}
        after = {"platform_family": "unix", "cases": [{"id": "help/new"}]}
        self.assertEqual(len(cli_contract.differences(before, after)), 2)

    def test_completion_preserves_duplicate_candidates_and_raw_snapshot(self):
        before = {"id": "completion", "comparison": "completion-lines", "stdout": "b\na\na\n"}
        reordered = dict(before, stdout="a\nb\na\n")
        removed_duplicate = dict(before, stdout="b\na\n")
        self.assertEqual(cli_contract.comparison_view(before), cli_contract.comparison_view(reordered))
        self.assertNotEqual(cli_contract.comparison_view(before), cli_contract.comparison_view(removed_duplicate))
        self.assertEqual(before["stdout"], "b\na\na\n")

    def test_reviewed_delta_applies_to_exact_case_fields_only(self):
        old = {"id": "health", "exit_code": 2, "stdout": "", "stderr": "panic\n", "side_effects": []}
        new = dict(old, exit_code=1, stderr="invalid datatype\nSUPPORTED FLAGS:\n")
        before = {"platform_family": "unix", "cases": [old]}
        after = {"platform_family": "unix", "cases": [new]}
        changes = {"health": {"reason": "documented old parser panic", "fields_before": {"exit_code": 2, "stderr": "panic\n"},
                               "fields_after": {"exit_code": 1, "stderr": new["stderr"]}}}
        self.assertFalse(cli_contract.differences(before, after, changes))
        after["cases"][0] = dict(new, stdout="unexpected output")
        self.assertTrue(cli_contract.differences(before, after, changes))
        with self.assertRaises(ValueError):
            cli_contract.differences(before, after, {"unrelated": changes["health"]})

    def test_manifest_and_baseline_case_ids_are_unique(self):
        manifest = cli_contract.load(cli_contract.CONTRACT_DIR / "cases.json")
        baseline = cli_contract.load(cli_contract.CONTRACT_DIR / "baseline.json")
        for cases in (manifest["cases"], baseline["cases"]):
            ids = [case["id"] for case in cases]
            self.assertEqual(len(ids), len(set(ids)))
        self.assertEqual(baseline["source"]["cli"]["version"], "v1.24.2")
        self.assertEqual(len(baseline["source"]["commit"]), 40)
        self.assertEqual(baseline["manifest_sha256"], hashlib.sha256(
            (cli_contract.CONTRACT_DIR / "cases.json").read_bytes()).hexdigest())

    def test_snapshot_case_has_isolated_home_and_captures_files(self):
        if os.name == "nt":
            self.skipTest("shell probe is Unix-only; real binary comparison runs on all platforms")
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / "probe"
            binary.write_text('#!/bin/sh\nprintf "home=%s\\n" "$HOME"\nprintf "failure\\n" >&2\nprintf data > created.txt\nexit 7\n')
            binary.chmod(0o755)
            result = cli_contract.run_case(binary, "probe", {"id": "isolation", "argv": []})
        self.assertEqual(result["exit_code"], 7)
        self.assertEqual(result["stdout"], "home={sandbox}/home\n")
        self.assertEqual(result["stderr"], "failure\n")
        self.assertEqual(result["side_effects"][0]["path"], "created.txt")


PROJECT = cli_contract.load(cli_contract.CONTRACT_DIR / "cases.json")["project"]
CANDIDATE = os.environ.get("CLI_TEST_BINARY") or os.environ.get(PROJECT.upper() + "_TEST_BINARY")


@unittest.skipUnless(CANDIDATE, "set CLI_TEST_BINARY (or the project TEST_BINARY) to compare a real candidate")
class CLIContractCandidateTests(unittest.TestCase):
    def test_candidate_matches_archived_baseline(self):
        baseline = cli_contract.CONTRACT_DIR / "baseline.json"
        before = baseline.read_bytes()
        command = [sys.executable, str(cli_contract.ROOT / "buildscripts" / "cli_contract.py"),
                   "check", "--binary", str(Path(CANDIDATE).resolve())]
        if os.environ.get("CLI_BASELINE_BINARY"):
            command += ["--baseline-binary", str(Path(os.environ["CLI_BASELINE_BINARY"]).resolve())]
        result = subprocess.run(command,
                                text=True, capture_output=True, timeout=300)
        self.assertEqual(baseline.read_bytes(), before, "comparison must never accept candidate snapshots")
        self.assertEqual(result.returncode, 0, result.stderr[:16000])


if __name__ == "__main__":
    unittest.main()
