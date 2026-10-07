"""Runner unit tests plus an opt-in real candidate/baseline comparison."""
import json
import hashlib
import io
import os
from pathlib import Path, PureWindowsPath
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import cli_contract


class CLIContractRunnerTests(unittest.TestCase):
    def baseline_identity_fixture(self, project="otterio"):
        module = "github.com/soulteary/otterio" if project == "otterio" else "github.com/soulteary/mc"
        sdk = "v0.0.0-20261004215341-be8596f0d69d"
        info = f"probe: go1.27.1\n\tpath\t{module}\n\tmod\t{module}\t(devel)\n\tdep\tgithub.com/minio/cli\tv1.24.2\n"
        if project == "oc":
            info += f"\tdep\tgithub.com/soulteary/otterio\t{sdk}\n"
        expected = {"project": project, "source": {"build_info": info, "commit": "a" * 40,
                    "binary_sha256": "saved-binary", "sdk_server_module": sdk if project == "oc" else "(devel)"}}
        vcs = "\tbuild\tvcs=git\n\tbuild\tvcs.revision=" + "a" * 40 + "\n\tbuild\tvcs.modified=false\n"
        return expected, info, vcs

    def test_clean_checkout_main_module_pseudoversion_matches_fixed_source(self):
        for project in ("otterio", "oc"):
            with self.subTest(project=project):
                expected, info, vcs = self.baseline_identity_fixture(project)
                info = info.replace("(devel)", "v0.0.0-20261008000000-aaaaaaaaaaaa")
                cli_contract.validate_baseline_identity(expected, info + vcs, "different-platform-binary")

    def test_baseline_rejects_dirty_or_wrong_revision_and_wrong_cli(self):
        expected, info, vcs = self.baseline_identity_fixture()
        for changed in (vcs.replace("false", "true"), vcs.replace("a" * 40, "b" * 40),
                        vcs.replace("vcs=git", "vcs=hg"), vcs.replace("\tbuild\tvcs.modified=false\n", "")):
            with self.subTest(vcs=changed), self.assertRaises(ValueError):
                cli_contract.validate_baseline_identity(expected, info + changed, "different-binary")
        with self.assertRaises(ValueError):
            cli_contract.validate_baseline_identity(expected, info.replace("v1.24.2", "v1.24.1") + vcs, "different-binary")

    def test_baseline_without_vcs_requires_the_saved_archive_binary(self):
        expected, info, _ = self.baseline_identity_fixture()
        cli_contract.validate_baseline_identity(expected, info, "saved-binary")
        with self.assertRaises(ValueError):
            cli_contract.validate_baseline_identity(expected, info, "unverified-binary")
        with self.assertRaises(ValueError):
            cli_contract.validate_baseline_identity(expected, info.replace("github.com/soulteary/otterio", "example.com/other"), "saved-binary")

    def test_oc_sdk_pin_is_strict_even_when_source_commit_matches(self):
        expected, info, vcs = self.baseline_identity_fixture("oc")
        with self.assertRaises(ValueError):
            cli_contract.validate_baseline_identity(expected, info.replace(expected["source"]["sdk_server_module"], "v0.0.0-new") + vcs,
                                                   "different-platform-binary")

    def test_baseline_rejects_replacements_even_with_original_versions_and_clean_vcs(self):
        expected, info, vcs = self.baseline_identity_fixture("oc")
        for replacement in ("\t=>\texample.com/cli-fork\tv1.24.2\th1:changed\n",
                            "\t=>\t../otterio-local\t(devel)\n"):
            with self.subTest(replacement=replacement), self.assertRaisesRegex(ValueError, "replacements"):
                cli_contract.validate_baseline_identity(expected, info + replacement + vcs, "different-platform-binary")

    def test_initial_recording_rejects_replaced_cli_before_creating_expected_results(self):
        _, info, _ = self.baseline_identity_fixture()
        with tempfile.TemporaryDirectory() as directory:
            contract = Path(directory)
            stderr = io.StringIO()
            argv = ["cli_contract", "record", "--binary", "unused", "--source-ref", "a" * 40,
                    "--source-tree", str(contract)]
            with patch.object(cli_contract, "CONTRACT_DIR", contract), patch.object(sys, "argv", argv), \
                    patch.object(cli_contract, "binary_build_info", return_value=info + "\t=>\t../cli-local\t(devel)\n"), \
                    patch.object(sys, "stderr", stderr), self.assertRaises(SystemExit) as raised:
                cli_contract.main()
            self.assertEqual(raised.exception.code, 2)
            self.assertIn("must not contain dependency replacements", stderr.getvalue())
            self.assertFalse((contract / "baseline.json").exists())

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
        self.assertEqual(baseline["manifest_sha256"], cli_contract.catalog_sha256(
            cli_contract.CONTRACT_DIR / "cases.json"))

    def test_catalog_digest_ignores_only_crlf_checkout_conversion(self):
        content = b'{"case": "--help"}\n'
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "cases.json"
            path.write_bytes(content)
            reviewed = cli_contract.catalog_sha256(path)
            self.assertEqual(reviewed, hashlib.sha256(content).hexdigest())
            path.write_bytes(content.replace(b"\n", b"\r\n"))
            self.assertEqual(cli_contract.catalog_sha256(path), reviewed)
            for changed in (content.replace(b"--help", b"--version"), content + b" ",
                            content.replace(b"\n", b"\r")):
                with self.subTest(changed=changed):
                    path.write_bytes(changed)
                    self.assertNotEqual(cli_contract.catalog_sha256(path), reviewed)

    def test_check_accepts_crlf_catalog_and_rejects_real_catalog_change(self):
        content = (cli_contract.CONTRACT_DIR / "cases.json").read_bytes().replace(b"\r\n", b"\n")
        baseline = cli_contract.load(cli_contract.CONTRACT_DIR / "baseline.json")
        with tempfile.TemporaryDirectory() as directory:
            contract = Path(directory)
            (contract / "baseline.json").write_text(json.dumps(baseline))
            path = contract / "cases.json"
            path.write_bytes(content.replace(b"\n", b"\r\n"))
            argv = ["cli_contract", "check", "--binary", "unused"]
            with patch.object(cli_contract, "CONTRACT_DIR", contract), patch.object(sys, "argv", argv), \
                    patch.object(cli_contract, "capture", return_value=baseline) as capture, \
                    patch.object(sys, "stdout", io.StringIO()):
                self.assertEqual(cli_contract.main(), 0)
                capture.assert_called_once_with("unused")
            path.write_bytes(content + b" ")
            stderr = io.StringIO()
            with patch.object(cli_contract, "CONTRACT_DIR", contract), patch.object(sys, "argv", argv), \
                    patch.object(cli_contract, "capture") as capture, patch.object(sys, "stderr", stderr), \
                    self.assertRaises(SystemExit) as raised:
                cli_contract.main()
            self.assertEqual(raised.exception.code, 2)
            self.assertIn("case catalog differs from the reviewed baseline", stderr.getvalue())
            capture.assert_not_called()

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
