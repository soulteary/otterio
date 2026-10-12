"""Exercise release packaging through a fake compiler and a real clean Git tree."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "buildscripts/build-release.py"
TAG = "RELEASE.2026-10-13T01-00-00Z"
NAMES = {
    "otterio-linux-amd64", "otterio-linux-arm64", "otterio-linux-ppc64le",
    "otterio-darwin-amd64", "otterio-darwin-arm64", "otterio-windows-amd64.exe",
}
STUB = r'''
import json, os, pathlib, sys
args = sys.argv[1:]
env = {k: os.environ.get(k) for k in (
    "GOOS", "GOARCH", "CGO_ENABLED", "GO111MODULE", "OTTERIO_RELEASE",
    "OTTERIO_BUILD_COMMIT", "OTTERIO_HOTFIX")}
with open(os.environ["GO_CALLS"], "a") as stream:
    stream.write(json.dumps(dict(args=args, env=env, cwd=os.getcwd())) + "\n")
if args[:2] == ["run", "buildscripts/gen-ldflags.go"]:
    print("-s -w -X fixture.Version=" + args[2])
elif args[0] == "build":
    if os.environ.get("FAIL_TARGET") == env["GOOS"] + "/" + env["GOARCH"]:
        sys.exit(23)
    path = pathlib.Path(args[args.index("-o") + 1])
    path.write_text(json.dumps(env) + "\n")
    if os.environ.get("MUTATE_SOURCE"):
        pathlib.Path("untracked-during-build").write_text("changed")
else:
    sys.exit("unexpected compiler invocation")
'''


class ReleaseBuildTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.source = self.root / "source"
        self.source.mkdir()
        (self.source / "go.mod").write_text("module releasefixture\n")
        (self.source / "main.go").write_text("package main\nfunc main() {}\n")
        for args in (("init", "-q"), ("config", "user.email", "fixture@example.invalid"),
                     ("config", "user.name", "Release fixture"), ("add", "."),
                     ("-c", "commit.gpgsign=false", "commit", "-qm", "fixture")):
            subprocess.run(["git", *args], cwd=self.source, check=True,
                           capture_output=True, text=True)
        self.sha = subprocess.check_output(["git", "rev-parse", "HEAD"],
                                           cwd=self.source, text=True).strip()
        self.output = self.root / "output"
        tools = self.root / "tools"
        tools.mkdir()
        go = tools / "go"
        go.write_text("#!" + sys.executable + "\n" + STUB)
        go.chmod(0o755)
        self.log = self.root / "go-calls.jsonl"
        self.env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
                        GO_CALLS=str(self.log), PYTHONDONTWRITEBYTECODE="1",
                        GOOS="windows", GOARCH="arm64", OTTERIO_HOTFIX="unwanted",
                        OTTERIO_BUILD_COMMIT="f" * 40)

    def run_build(self, tag=TAG, output=None, **settings):
        return subprocess.run(
            [sys.executable, str(SCRIPT), tag, "--output", str(output or self.output),
             "--source-dir", str(self.source)],
            env=dict(self.env, **settings), text=True, capture_output=True, timeout=20,
        )

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_six_original_targets_and_flags_have_exact_checksums_including_manifest(self):
        result = self.run_build()
        self.assertEqual(result.returncode, 0, result.stderr)
        checksum = f"otterio-{TAG}-checksums.txt"
        self.assertEqual({p.name for p in self.output.iterdir()}, NAMES | {checksum, "release-manifest.json"})
        entries = dict((name, digest) for digest, name in
                       (line.split("  ") for line in (self.output / checksum).read_text().splitlines()))
        self.assertEqual(set(entries), NAMES | {"release-manifest.json"})
        for name, digest in entries.items():
            self.assertEqual(hashlib.sha256((self.output / name).read_bytes()).hexdigest(), digest)
        manifest = json.loads((self.output / "release-manifest.json").read_text())
        self.assertEqual(manifest, dict(schema_version=1, tag=TAG, source_sha=self.sha, images=[]))
        calls = self.calls()
        self.assertEqual(len(calls), 7)
        self.assertEqual(calls[0]["args"], ["run", "buildscripts/gen-ldflags.go", "2026-10-13T01:00:00Z"])
        self.assertIsNone(calls[0]["env"]["GOOS"])
        self.assertIsNone(calls[0]["env"]["GOARCH"])
        self.assertEqual({(c["env"]["GOOS"], c["env"]["GOARCH"]) for c in calls[1:]},
                         {("linux", "amd64"), ("linux", "arm64"), ("linux", "ppc64le"),
                          ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64")})
        for call in calls:
            self.assertEqual(call["cwd"], str(self.source))
            self.assertEqual(call["env"]["OTTERIO_BUILD_COMMIT"], self.sha)
            self.assertEqual(call["env"]["OTTERIO_RELEASE"], "RELEASE")
            self.assertIsNone(call["env"]["OTTERIO_HOTFIX"])
        for call in calls[1:]:
            self.assertEqual(call["args"][:6], ["build", "-tags", "kqueue", "-trimpath", "-ldflags",
                                               "-s -w -X fixture.Version=2026-10-13T01:00:00Z"])
            self.assertEqual(call["env"]["CGO_ENABLED"], "0")
            self.assertEqual(call["env"]["GO111MODULE"], "on")
        self.assertEqual(subprocess.check_output(["git", "status", "--porcelain"], cwd=self.source), b"")

    def test_invalid_tag_is_rejected_before_compilation_or_output(self):
        result = self.run_build("RELEASE.invalid")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertFalse(self.output.exists())

    def test_dirty_source_is_rejected_before_compilation(self):
        (self.source / "untracked").write_text("dirty")
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertFalse(self.output.exists())

    def test_repository_output_cannot_dirty_later_binary_vcs_metadata(self):
        result = self.run_build(output=self.source / "dist")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertFalse((self.source / "dist").exists())

    def test_existing_assets_are_never_mixed_with_new_binaries(self):
        self.output.mkdir()
        stale = self.output / "stale"
        stale.write_bytes(b"retained")
        result = self.run_build()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertEqual(stale.read_bytes(), b"retained")

    def test_failed_compile_does_not_emit_a_complete_inventory(self):
        result = self.run_build(FAIL_TARGET="linux/arm64")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.output / "release-manifest.json").exists())
        self.assertFalse((self.output / f"otterio-{TAG}-checksums.txt").exists())

    def test_changed_source_during_build_does_not_emit_a_complete_inventory(self):
        result = self.run_build(MUTATE_SOURCE="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.output / "release-manifest.json").exists())
        self.assertFalse((self.output / f"otterio-{TAG}-checksums.txt").exists())


if __name__ == "__main__":
    unittest.main()
