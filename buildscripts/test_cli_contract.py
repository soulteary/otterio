"""Runner unit tests plus an opt-in real candidate/baseline comparison."""
import json
import hashlib
import io
import base64
import os
from pathlib import Path, PureWindowsPath
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from unittest.mock import Mock

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

    def test_worker_preserves_bytes_exit_code_and_closed_stdin(self):
        with tempfile.TemporaryDirectory() as directory:
            command = [sys.executable, "-c", "import sys; assert not sys.stdin.read(); "
                       "sys.stdout.buffer.write(b'output\\n'); sys.stderr.buffer.write(b'error\\xff\\n'); sys.exit(7)"]
            request = {"command": command, "env": cli_contract.environment(Path(directory)), "cwd": directory}
            result = subprocess.run([sys.executable, str(cli_contract.ROOT / "buildscripts/cli_contract.py"), "--process-worker"],
                                    input=json.dumps(request).encode(), capture_output=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            actual = json.loads(result.stdout)
            self.assertEqual(actual["exit_code"], 7)
            self.assertEqual(base64.b64decode(actual["stdout"]), b"output\n")
            self.assertEqual(base64.b64decode(actual["stderr"]), b"error\xff\n")

    def test_cleanup_retries_only_transient_windows_sharing_violation(self):
        error = OSError("locked image")
        error.winerror = 32
        temporary = Mock()
        temporary.cleanup.side_effect = [error, None]
        with patch.object(cli_contract.time, "monotonic", side_effect=[0, 0.1]), patch.object(cli_contract.time, "sleep") as sleep:
            cli_contract.cleanup_case(temporary, "sharing-case", windows=True)
        self.assertEqual(temporary.cleanup.call_count, 2)
        sleep.assert_called_once()
        for windows, failure in ((False, error), (True, PermissionError("real permission failure"))):
            temporary = Mock()
            temporary.cleanup.side_effect = failure
            with self.subTest(windows=windows), self.assertRaisesRegex(RuntimeError, "sharing-case cleanup failed"):
                cli_contract.cleanup_case(temporary, "sharing-case", windows=windows)
            temporary.cleanup.assert_called_once()

    def test_cleanup_permanent_lock_fails_with_case_identity(self):
        error = OSError("locked image")
        error.winerror = 32
        temporary = Mock()
        temporary.cleanup.side_effect = error
        with patch.object(cli_contract.time, "monotonic", side_effect=[0, 0.1, 2.1]), \
                patch.object(cli_contract.time, "sleep"), self.assertRaisesRegex(RuntimeError, "locked-case cleanup failed"):
            cli_contract.cleanup_case(temporary, "locked-case", windows=True)
        self.assertEqual(temporary.cleanup.call_count, 2)

    def test_windows_assignment_failure_reaps_actual_blocked_worker(self):
        job = Mock()
        job.assign.side_effect = OSError("assignment refused")
        original = subprocess.Popen
        workers = []
        def spawn(*args, **kwargs):
            worker = original(*args, **kwargs)
            workers.append(worker)
            return worker
        with tempfile.TemporaryDirectory() as directory, patch.object(cli_contract, "WindowsJob", return_value=job), \
                patch.object(cli_contract.subprocess, "Popen", side_effect=spawn):
            with self.assertRaisesRegex(OSError, "assignment refused"):
                cli_contract.windows_run([sys.executable, "-c", "raise AssertionError('must not run')"], {}, directory, 5)
        job.close.assert_called_once()
        self.assertEqual(len(workers), 1)
        self.assertIsNotNone(workers[0].poll(), "blocked worker leaked after failed assignment")
        self.assertTrue(all(pipe.closed for pipe in (workers[0].stdin, workers[0].stdout, workers[0].stderr)))

    def test_windows_terminate_failure_closes_job_before_bounded_worker_wait(self):
        events = []
        job = Mock()
        job.terminate.side_effect = OSError("termination refused")
        job.close.side_effect = lambda: events.append("job closed")
        worker = Mock()
        worker.communicate.side_effect = subprocess.TimeoutExpired("worker", 1)
        worker.poll.return_value = None
        worker.wait.side_effect = lambda timeout: events.append(("worker waited", timeout))
        with patch.object(cli_contract, "WindowsJob", return_value=job), \
                patch.object(cli_contract.subprocess, "Popen", return_value=worker), \
                self.assertRaisesRegex(OSError, "termination refused"):
            cli_contract.windows_run(["unused"], {}, ".", 1)
        self.assertEqual(events, ["job closed", ("worker waited", 5)])
        worker.kill.assert_called_once()
        for pipe in (worker.stdin, worker.stdout, worker.stderr):
            pipe.close.assert_called_once()

    @unittest.skipUnless(os.name == "nt", "native Windows process tree regression")
    def test_windows_job_preserves_streams_and_normal_exit(self):
        with tempfile.TemporaryDirectory() as directory:
            for iteration in range(20):
                with self.subTest(iteration=iteration):
                    result = cli_contract.bounded_run([sys.executable, "-c", "import sys; print('out'); print('err', file=sys.stderr); sys.exit(7)"],
                                                       cli_contract.environment(Path(directory)), directory, 10)
                    self.assertEqual(result, (7, "out\r\n", "err\r\n"))

    @unittest.skipUnless(os.name == "nt", "native Windows process tree regression")
    def test_windows_job_detects_and_reaps_background_child(self):
        self.windows_child_regression(timeout=False)

    @unittest.skipUnless(os.name == "nt", "native Windows process tree regression")
    def test_windows_timeout_reaps_the_entire_process_tree(self):
        self.windows_child_regression(timeout=True)

    @unittest.skipUnless(os.name == "nt", "native Windows sharing violation regression")
    def test_windows_cleanup_waits_for_released_file_handle(self):
        import ctypes
        import threading
        from ctypes import wintypes
        temporary = tempfile.TemporaryDirectory()
        path = Path(temporary.name) / "locked.exe"
        path.write_bytes(b"fixture")
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.CreateFileW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD, ctypes.c_void_p,
                                      wintypes.DWORD, wintypes.DWORD, wintypes.HANDLE]
        kernel.CreateFileW.restype = wintypes.HANDLE
        kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        handle = kernel.CreateFileW(str(path), 0x80000000, 1, None, 3, 0x80, None)
        self.assertNotEqual(handle, ctypes.c_void_p(-1).value)
        released = threading.Event()
        try:
            with self.assertRaises(OSError) as raised:
                path.unlink()
            self.assertEqual(raised.exception.winerror, 32)
            def release():
                cli_contract.time.sleep(0.15)
                kernel.CloseHandle(handle)
                released.set()
            worker = threading.Thread(target=release)
            worker.start()
            try:
                cli_contract.cleanup_case(temporary, "native-sharing-case")
            finally:
                worker.join(timeout=3)
            self.assertFalse(worker.is_alive())
            self.assertFalse(Path(temporary.name).exists())
        finally:
            if not released.is_set():
                kernel.CloseHandle(handle)
            temporary.cleanup()

    def windows_child_regression(self, timeout):
        import ctypes
        from ctypes import wintypes
        with tempfile.TemporaryDirectory() as directory:
            script = "import subprocess,sys,time; from pathlib import Path; " \
                     "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'], " \
                     "stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL); " \
                     "Path('child.pid').write_text(str(child.pid)); " + ("time.sleep(30)" if timeout else "print('root exited')")
            expected = "timed out" if timeout else "left running child processes"
            with self.assertRaisesRegex(RuntimeError, expected):
                cli_contract.bounded_run([sys.executable, "-c", script], cli_contract.environment(Path(directory)),
                                          directory, 2 if timeout else 10)
            pid = int((Path(directory) / "child.pid").read_text())
            kernel = ctypes.WinDLL("kernel32", use_last_error=True)
            kernel.OpenProcess.argtypes, kernel.OpenProcess.restype = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD], wintypes.HANDLE
            kernel.WaitForSingleObject.argtypes, kernel.WaitForSingleObject.restype = [wintypes.HANDLE, wintypes.DWORD], wintypes.DWORD
            kernel.CloseHandle.argtypes = [wintypes.HANDLE]
            handle = kernel.OpenProcess(0x100000, False, pid)  # SYNCHRONIZE
            if handle:
                try:
                    self.assertEqual(kernel.WaitForSingleObject(handle, 0), 0, "leaked child remained alive")
                finally:
                    kernel.CloseHandle(handle)
            else:
                self.assertEqual(ctypes.get_last_error(), 87, "could not inspect leaked child termination")

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
