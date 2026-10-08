"""Exercise the real Mint installer offline; fake Go records downloads/builds."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
INSTALLER = ROOT / "mint/build/minio-go/install.sh"
SDK = "github.com/soulteary/otterio-sdk/v7"


@unittest.skipUnless(os.name == "posix" and shutil.which("bash"), "POSIX Mint installer")
class MintGoInstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="mint installer ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.core = self.root / "core"
        self.module = self.core / "minio-go"
        self.module.mkdir(parents=True)
        self.mod = self.module / "go.mod"
        self.sums = self.module / "go.sum"
        self.mod.write_text(f"module mint.minio.io/minio-go\n\ngo 1.27.1\n\nrequire {SDK} v7.3.1 // indirect\n")
        self.sums.write_text("committed checksums\n")
        self.source = self.root / "verified sdk source"
        self.source.mkdir()
        (self.source / "functional_tests.go").write_text(
            f'package main\nimport minio "{SDK}"\nfunc main() {{ _ = minio.New }}\n'
        )
        self.commands = self.root / "commands.jsonl"
        self.tools = self.root / "tools"
        self.tools.mkdir()
        fake_go = self.tools / "go"
        fake_go.write_text('''#!/usr/bin/env python3
import json, os, pathlib, re, sys
args = sys.argv[1:]
module = pathlib.Path.cwd()
with open(os.environ["MINT_FAKE_LOG"], "a") as log:
    log.write(json.dumps({"args": args, "cwd": str(module), "env": {
        name: os.environ.get(name) for name in ("GO111MODULE", "GOWORK", "CGO_ENABLED")
    }}) + "\\n")
sdk = "github.com/soulteary/otterio-sdk/v7"
match = re.search(re.escape(sdk) + r" (v\\S+)", (module / "go.mod").read_text())
if not match:
    sys.exit("missing pinned SDK")
version = match[1]
if args[:4] == ["list", "-mod=readonly", "-m", "-f"] and args[-1] == sdk:
    if args[4] == "{{.Dir}}":
        print(os.environ["MINT_FAKE_SOURCE"])
    else:
        print("replaced" if os.environ.get("MINT_FAKE_REPLACE") else version)
elif args == ["mod", "download", sdk + "@" + version]:
    if os.environ.get("MINT_FAKE_DOWNLOAD_FAIL"):
        sys.exit("module download/checksum failed")
elif args and args[0] == "build":
    if "-mod=readonly" not in args:
        with open(module / "go.mod", "a") as mod:
            mod.write("require github.com/minio/minio-go/v7 v7.9.9\\n")
        sys.exit("mutable build would pollute module")
    if "github.com/minio/" in (module / "main.go").read_text():
        sys.exit("readonly build rejects upstream imports")
    (module / args[args.index("-o") + 1]).write_text("built harness")
elif args == ["version", "-m", "minio-go"]:
    inventory = os.environ.get("MINT_FAKE_INVENTORY")
    print(inventory if inventory is not None else "\\tdep\\t" + sdk + "\\t" + version + "\\th1:fixture")
else:
    sys.exit("unexpected Go command: " + repr(args))
''')
        fake_go.chmod(0o755)
        curl = self.tools / "curl"
        curl.write_text("#!/bin/sh\necho unexpected HTTP/latest lookup >&2\nexit 99\n")
        curl.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.tools) + os.pathsep + os.environ.get("PATH", ""),
                        MINT_RUN_CORE_DIR=str(self.core), MINT_FAKE_LOG=str(self.commands),
                        MINT_FAKE_SOURCE=str(self.source), GOWORK="unexpected-parent-workspace")

    def install(self, **env):
        before = (self.mod.read_bytes(), self.sums.read_bytes())
        result = subprocess.run(["bash", str(INSTALLER)], cwd=self.root,
                                env=dict(self.env, **env), capture_output=True, text=True, timeout=10)
        self.assertEqual(before, (self.mod.read_bytes(), self.sums.read_bytes()))
        return result

    def calls(self):
        return [json.loads(line) for line in self.commands.read_text().splitlines()]

    def test_uses_verified_fork_source_and_manifest_pin(self):
        for version in ("v7.3.1", "v7.8.9"):
            with self.subTest(version=version):
                self.commands.unlink(missing_ok=True)
                self.mod.write_text(f"module mint.minio.io/minio-go\n\ngo 1.27.1\n\nrequire {SDK} {version}\n")
                result = self.install()
                self.assertEqual(result.returncode, 0, result.stderr)
                calls = self.calls()
                self.assertIn(["mod", "download", SDK + "@" + version], [call["args"] for call in calls])
                self.assertIn(["build", "-mod=readonly", "-o", "minio-go", "main.go"], [call["args"] for call in calls])
                self.assertEqual((self.module / "main.go").read_bytes(), (self.source / "functional_tests.go").read_bytes())
                self.assertTrue((self.module / "minio-go").exists())
                for call in calls:
                    self.assertEqual(Path(call["cwd"]), self.module.resolve())
                    self.assertEqual(call["env"], {"GO111MODULE": "on", "GOWORK": "off", "CGO_ENABLED": "0"})

    def test_replaced_sdk_is_rejected_before_download(self):
        result = self.install(MINT_FAKE_REPLACE="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unreplaced", result.stderr)
        self.assertEqual(len(self.calls()), 1)

    def test_missing_sdk_pin_does_not_download_or_build(self):
        self.mod.write_text("module mint.minio.io/minio-go\n\ngo 1.27.1\n")
        self.assertNotEqual(self.install().returncode, 0)
        self.assertEqual(len(self.calls()), 1)

    def test_failed_download_does_not_use_stale_source(self):
        (self.module / "main.go").write_text("stale source")
        result = self.install(MINT_FAKE_DOWNLOAD_FAIL="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual((self.module / "main.go").read_text(), "stale source")
        self.assertFalse(any(call["args"][0] == "build" for call in self.calls()))

    def test_missing_functional_source_does_not_build(self):
        (self.source / "functional_tests.go").unlink()
        self.assertNotEqual(self.install().returncode, 0)
        self.assertFalse(any(call["args"][0] == "build" for call in self.calls()))

    def test_upstream_source_cannot_add_a_module_requirement(self):
        (self.source / "functional_tests.go").write_text('package main\nimport "github.com/minio/minio-go/v7"\n')
        result = self.install()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("readonly build rejects upstream imports", result.stderr)
        self.assertFalse((self.module / "minio-go").exists())

    def test_inventory_requires_exact_fork_sdk_and_no_upstream_or_replacement(self):
        inventories = (
            "dep github.com/minio/minio-go/v7 v7.3.1 h1:fixture",
            f"dep {SDK} v7.3.0 h1:fixture",
            f"dep {SDK} v7.3.1 h1:fixture\\ndep github.com/minio/md5-simd v1.1.0 h1:fixture",
            f"dep {SDK} v7.3.1 h1:fixture\\n=> ../local-sdk (devel)",
        )
        for inventory in inventories:
            with self.subTest(inventory=inventory):
                result = self.install(MINT_FAKE_INVENTORY=inventory.replace("\\n", "\n"))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Mint binary must link the pinned OtterIO SDK", result.stderr)


if __name__ == "__main__":
    unittest.main()
