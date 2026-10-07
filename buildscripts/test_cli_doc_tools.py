#!/usr/bin/env python3
"""Check both standalone xl.meta tools with disposable metadata fixtures."""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ENGLISH = CHINESE = None
BASELINE_ENGLISH = BASELINE_CHINESE = None
METADATA = b"XL2 \x01\x00\x00\x00\x81\xa1a\x01"


class DocumentationCLI(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="otterio-xl-meta-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.metadata = self.root / "xl.meta"
        self.metadata.write_bytes(METADATA)
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith(("OTTERIO_", "MINIO_"))}
        self.env["HOME"] = str(self.root)

    def run_tool(self, binary, *args, data=None):
        return subprocess.run([str(binary), *args], input=data, cwd=self.root,
                              env=self.env, capture_output=True, timeout=15)

    def assert_metadata(self, result):
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertEqual(result.stderr, b"")
        self.assertEqual(json.loads(result.stdout), {"a": 1})

    def test_english_default_file(self):
        self.assert_metadata(self.run_tool(ENGLISH))

    def test_chinese_no_arguments_is_help(self):
        before = sorted(path.name for path in self.root.iterdir())
        result = self.run_tool(CHINESE)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stderr, b"")
        self.assertTrue(result.stdout.startswith(b"NAME:\n"), result.stdout)
        self.assertIn(b"METAFILES...", result.stdout)
        self.assertEqual(before, sorted(path.name for path in self.root.iterdir()))

    def test_files_and_stdin(self):
        for binary in (ENGLISH, CHINESE):
            with self.subTest(binary=binary):
                self.assert_metadata(self.run_tool(binary, "xl.meta"))
                self.assert_metadata(self.run_tool(binary, "-", data=METADATA))

    def test_ndjson(self):
        for binary in (ENGLISH, CHINESE):
            with self.subTest(binary=binary):
                result = self.run_tool(binary, "--ndjson", "xl.meta", "xl.meta")
                self.assertEqual(result.returncode, 0, result.stderr.decode())
                self.assertEqual(result.stderr, b"")
                self.assertEqual([json.loads(line) for line in result.stdout.splitlines()],
                                 [{"a": 1}, {"a": 1}])

    def test_help_alias_and_no_writes(self):
        for binary in (ENGLISH, CHINESE):
            for alias in ("--help", "-h"):
                with self.subTest(binary=binary, alias=alias):
                    result = self.run_tool(binary, alias)
                    self.assertEqual(result.returncode, 0)
                    self.assertEqual(result.stderr, b"")
                    self.assertTrue(result.stdout.startswith(b"NAME:\n"), result.stdout)
                    self.assertIn(b"--ndjson", result.stdout)
                    self.assertNotIn(b"<no value>", result.stdout)
                    self.assertEqual(self.metadata.read_bytes(), METADATA)

    def test_errors_do_not_add_help(self):
        invalid = self.root / "invalid.meta"
        invalid.write_bytes(b"BAD!\x01\x00\x00\x00\x80")
        for binary in (ENGLISH, CHINESE):
            for args, message in ((["invalid.meta"], b"unknown XLv2 header"),
                                  (["missing.meta"], b"missing.meta"),
                                  (["--unknown"], b"flag provided but not defined")):
                with self.subTest(binary=binary, args=args):
                    result = self.run_tool(binary, *args)
                    self.assertEqual(result.returncode, 1)
                    self.assertIn(message, result.stderr)
                    self.assertNotIn(b"NAME:", result.stdout)

    def test_exact_archived_baseline(self):
        if BASELINE_ENGLISH is None:
            self.skipTest("archived tools were not supplied")
        (self.root / "invalid.meta").write_bytes(b"BAD!\x01\x00\x00\x00\x80")
        (self.root / "--help").write_bytes(METADATA)
        cases = ([], ["--help"], ["-h"], ["--version"], ["--version=false"], ["--unknown"],
                 ["--help", "--unknown"], ["--help", "--ndjson=invalid"], ["--help", "-h"],
                 ["--ndjson=invalid"], ["invalid.meta"], ["missing.meta"],
                 ["xl.meta"], ["--ndjson", "xl.meta", "xl.meta"],
                 ["xl.meta", "--ndjson"], ["--ndjson=false", "xl.meta"],
                 ["-"], ["--", "--help"])

        def normalize(data, binary):
            # Go's diagnostic log prefix contains wall-clock time. Preserve
            # all diagnostic content, whitespace, stream ownership and codes.
            data = re.sub(rb"(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} ", b"{log-time} ", data)
            return data.replace(binary.name.encode(), b"{tool}")

        for baseline, candidate in ((BASELINE_ENGLISH, ENGLISH),
                                    (BASELINE_CHINESE, CHINESE)):
            for args in cases:
                with self.subTest(binary=candidate, args=args):
                    old = self.run_tool(baseline, *args, data=METADATA)
                    new = self.run_tool(candidate, *args, data=METADATA)
                    self.assertEqual(new.returncode, old.returncode)
                    self.assertEqual(normalize(new.stdout, candidate), normalize(old.stdout, baseline))
                    self.assertEqual(normalize(new.stderr, candidate), normalize(old.stderr, baseline))


def main():
    global ENGLISH, CHINESE, BASELINE_ENGLISH, BASELINE_CHINESE
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--english", type=Path, required=True)
    parser.add_argument("--chinese", type=Path, required=True)
    parser.add_argument("--baseline-english", type=Path)
    parser.add_argument("--baseline-chinese", type=Path)
    args = parser.parse_args()
    ENGLISH, CHINESE = args.english.resolve(), args.chinese.resolve()
    if bool(args.baseline_english) != bool(args.baseline_chinese):
        parser.error("supply both archived tool binaries")
    if args.baseline_english:
        BASELINE_ENGLISH, BASELINE_CHINESE = args.baseline_english.resolve(), args.baseline_chinese.resolve()
    for binary in (ENGLISH, CHINESE, BASELINE_ENGLISH, BASELINE_CHINESE):
        if binary is None:
            continue
        if not binary.is_file():
            parser.error(f"tool does not exist: {binary}")
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(DocumentationCLI)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
