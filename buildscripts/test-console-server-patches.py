#!/usr/bin/env python3
"""Verify the management/storage split against the frozen P3 patch.

This exports a fixed Git revision into disposable directories; it never edits
its source checkout or Go module cache. Base and lifecycle builds/tests run in
separate directories. Additional feature/IAM patches may be supplied in order.
Lifecycle hardening patches apply only to the lifecycle profile, after the
frozen core equivalence check and before feature/IAM patches.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time

BASE = "6f6d0835ddff68020f1491c403b958fade22841f"
BASE_SELECTOR = (
    "TestBucketConfig|TestSelfCredentials|TestConfiguration|TestFiber|"
    "TestGetBucket|TestPutBucket|TestDeleteBucket|TestAccountInfo|TestBucketMetadata"
)
STORAGE_SELECTOR = (
    BASE_SELECTOR + "|TestBucketTarget|TestTransition|TestXLStorageInline|"
    "TestLifecycleTransition|TestLifecycleQueues|TestScannerLifecycle|TestExpiry|"
    "TestParseRestore|TestRestoreRequest|TestBeginRestore|TestRestoredVersion|"
    "TestPutObjectExpiry|TestFindFileInfoInQuorum|TestXLV2FormatData|"
    "TestErasureDeleteObjectBasic|TestErasureDeleteObjectsErasureSet|"
    "TestErasurePutObject|TestXLStorageReadFile|"
    "TestXLStorageHealsPendingTransitionMetadata|TestMonitorAndConnectEndpointsCanceled"
)
PROTECTED_FILES = (
    "cmd/admin-bucket-handlers.go", "cmd/api-headers.go", "cmd/bucket-targets.go",
    "cmd/bucket-lifecycle.go", "cmd/data-scanner.go", "cmd/object-api-datatypes.go",
    "cmd/object-api-interface.go", "cmd/object-handlers.go",
    "cmd/object-handlers-common.go", "cmd/erasure-object.go",
    "cmd/erasure-metadata.go", "cmd/erasure-healing.go",
    "cmd/erasure-healing-common.go", "cmd/erasure-sets.go",
    "cmd/xl-storage.go", "cmd/xl-storage-format-v2.go", "cmd/web-handlers.go",
    "pkg/bucket/lifecycle/filter.go", "pkg/bucket/lifecycle/lifecycle.go",
)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def inventory(root: Path) -> dict[str, str]:
    return {
        str(p.relative_to(root)): sha256(p.read_bytes())
        for p in sorted(root.rglob("*"))
        if p.is_file() and ".git" not in p.relative_to(root).parts
    }


def go_functions(data: str) -> dict[str, str]:
    # gofmt closes top-level function bodies in column zero, even when their
    # arguments span lines. Compare whole method bodies without parsing strings.
    functions = {}
    for match in re.finditer(r"(?ms)^func (.*?)^}\n", data):
        text = match.group(0)
        name = text.split("{", 1)[0].strip()
        functions[name] = text
    return functions


def assert_base_boundary(pin: Path, base: Path) -> dict:
    protected = {
        file: (pin / file).read_bytes() == (base / file).read_bytes()
        for file in PROTECTED_FILES
    }
    if not all(protected.values()):
        raise AssertionError(f"base modifies storage files: {[p for p, same in protected.items() if not same]}")
    bucket_only = {}
    for file in ("cmd/fs-v1.go", "cmd/erasure-server-pool.go"):
        original = go_functions((pin / file).read_text())
        actual = go_functions((base / file).read_text())
        changed = sorted(name for name in original.keys() | actual.keys()
                         if original.get(name) != actual.get(name))
        if len(changed) != 2 or any(
                not re.search(r"\b(?:MakeBucketWithLocation|DeleteBucket)\(", name)
                for name in changed):
            raise AssertionError(f"base changes object methods in {file}: {changed}")
        bucket_only[file] = changed
    return {"protectedFilesUnchanged": protected, "bucketMethodsOnly": bucket_only}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server-source", type=Path, required=True,
                        help="local server Git checkout, exported read-only at --base")
    parser.add_argument("--patch-dir", type=Path, required=True,
                        help="OC buildscripts directory containing the frozen P3 and core split patches")
    parser.add_argument("--base", default=BASE, choices=[BASE])
    parser.add_argument("--source-revision", default=None,
                        help="required fixed revision declaration for a pristine module directory without Git")
    parser.add_argument("--profile", choices=["base", "lifecycle", "all"], default="all")
    parser.add_argument("--extra-patch", action="append", type=Path, default=[],
                        help="additional feature/IAM patch, applied after the selected profile; repeat in order")
    parser.add_argument("--storage-extra-patch", action="append", type=Path, default=[],
                        help="lifecycle-only hardening patch, applied after core equivalence and before --extra-patch; repeat in order")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--work-dir", type=Path,
                        help="new directory for retained source, binary and log artifacts")
    parser.add_argument("--go", default="go")
    parser.add_argument("--skip-build", action="store_true")
    parser.add_argument("--skip-tests", action="store_true")
    parser.add_argument("--no-race", action="store_true")
    args = parser.parse_args()
    source = args.server_source.resolve()
    patches = args.patch_dir.resolve()
    output = args.output.resolve()
    work = args.work_dir.resolve() if args.work_dir else Path(tempfile.mkdtemp(prefix="console-server-patches-")).resolve()
    if args.work_dir:
        work.mkdir(parents=True, exist_ok=False)
    logs = work / "logs"
    logs.mkdir()
    report = {
        "dateUTC": dt.datetime.now(dt.timezone.utc).isoformat(), "status": "running",
        "base": args.base, "source": {"path": str(source)}, "artifactDirectory": str(work),
        "requestedProfile": args.profile, "patches": {}, "checks": [], "profiles": {},
        "runtimeTestsSkipped": args.skip_tests, "buildsSkipped": args.skip_build,
        "race": not args.no_race,
        "scope": "fixed-pin frozen-core equivalence, ordered profile patches and separate native validation",
    }
    sequence = 0

    def run(label: str, command: list[str], cwd: Path, env: dict | None = None) -> bytes:
        nonlocal sequence
        sequence += 1
        started = time.monotonic()
        completed = subprocess.run(command, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        log = logs / f"{sequence:02d}-{re.sub(r'[^a-z0-9]+', '-', label.lower()).strip('-')}.log"
        log.write_bytes(completed.stdout)
        report["checks"].append({
            "name": label, "command": command, "exitCode": completed.returncode,
            "status": "passed" if completed.returncode == 0 else "failed",
            "elapsedSeconds": round(time.monotonic() - started, 3),
            "logPath": str(log), "logSHA256": sha256(completed.stdout),
            "outputTail": completed.stdout.decode(errors="replace")[-4000:],
        })
        print(f"{label}: {'passed' if completed.returncode == 0 else 'failed'}", flush=True)
        if completed.returncode:
            raise RuntimeError(f"{label} failed; see {log}")
        return completed.stdout

    try:
        for name in ("console-server-p3.patch", "console-server-base.patch", "lifecycle-storage.patch"):
            file = patches / name
            report["patches"][name] = {"path": str(file), "sha256": sha256(file.read_bytes())}
        extra = [path.resolve() for path in args.extra_patch]
        report["extraPatches"] = [{"path": str(path), "sha256": sha256(path.read_bytes())} for path in extra]
        storage_extra = [path.resolve() for path in args.storage_extra_patch]
        report["storageExtraPatches"] = [
            {"path": str(path), "sha256": sha256(path.read_bytes())} for path in storage_extra
        ]
        pin = work / "pin"
        pin.mkdir()
        if (source / ".git").exists():
            resolved = subprocess.check_output(["git", "rev-parse", args.base + "^{commit}"], cwd=source).decode().strip()
            if resolved != args.base:
                raise AssertionError("Git resolved a different fixed base")
            report["source"].update({
                "type": "git-archive", "identityVerified": True, "archivedRevision": resolved,
                "checkoutHead": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source).decode().strip(),
            })
            archive = subprocess.check_output(["git", "archive", "--format=tar", resolved], cwd=source)
            with tarfile.open(fileobj=io.BytesIO(archive), mode="r:") as stream:
                # Reject archive paths escaping the temporary source directory.
                for member in stream.getmembers():
                    target = (pin / member.name).resolve()
                    if not target.is_relative_to(pin):
                        raise AssertionError("unsafe archive member")
                if hasattr(tarfile, "data_filter"):
                    stream.extractall(pin, filter="data")
                else:
                    stream.extractall(pin)
        else:
            if args.source_revision != args.base:
                raise AssertionError("a non-Git source needs --source-revision set to the fixed base")
            shutil.copytree(source, pin, dirs_exist_ok=True, ignore=shutil.ignore_patterns(".git"))
            report["source"].update({"type": "directory-copy", "identityVerified": False,
                                     "declaredRevision": args.source_revision})
        pristine = inventory(pin)
        base = work / "base"
        combined = work / "lifecycle"
        reference = work / "monolithic"
        for target in (base, reference):
            shutil.copytree(pin, target)
        run("base patch applies", ["git", "apply", "--check", str(patches / "console-server-base.patch")], base)
        run("apply base patch", ["git", "apply", str(patches / "console-server-base.patch")], base)
        report["baseBoundary"] = assert_base_boundary(pin, base)
        shutil.copytree(base, combined)
        run("storage patch applies after base", ["git", "apply", "--check", str(patches / "lifecycle-storage.patch")], combined)
        run("apply storage patch", ["git", "apply", str(patches / "lifecycle-storage.patch")], combined)
        run("frozen P3 reference applies", ["git", "apply", "--check", str(patches / "console-server-p3.patch")], reference)
        run("apply frozen P3 reference", ["git", "apply", str(patches / "console-server-p3.patch")], reference)
        actual, expected = inventory(combined), inventory(reference)
        mismatch = sorted(p for p in actual.keys() | expected.keys() if actual.get(p) != expected.get(p))
        report["jointByteEquivalent"] = not mismatch
        report["jointMismatches"] = mismatch
        if mismatch:
            raise AssertionError(f"split source differs from frozen P3: {mismatch}")
        report["moduleFilesUnchanged"] = all(
            pristine[file] == inventory(path)[file]
            for path in (base, combined, reference) for file in ("go.mod", "go.sum")
        )
        if not report["moduleFilesUnchanged"]:
            raise AssertionError("patches changed module files")
        report["sourceTreeSHA256"] = sha256(json.dumps(actual, sort_keys=True).encode())
        report["coreSourceTreeSHA256"] = report["sourceTreeSHA256"]
        report["sourceTreeSHA256Scope"] = "frozen P3 core before profile-specific extra patches"
        report["finalSourceTreeSHA256"] = {}
        report["sourcePaths"] = {"pin": str(pin), "base": str(base), "lifecycle": str(combined), "monolithic": str(reference)}
        env = os.environ.copy()
        env.setdefault("GOCACHE", str(work / "go-cache"))
        env.setdefault("GOTOOLCHAIN", "local")
        env.setdefault("GOFLAGS", "-mod=readonly")
        report["goVersion"] = run("Go version", [args.go, "version"], work, env).decode().strip()
        profiles = [("base", base, BASE_SELECTOR), ("lifecycle", combined, STORAGE_SELECTOR)]
        for name, directory, selector in profiles:
            if args.profile not in ("all", name):
                continue
            if extra:
                selector += "|^TestConsoleVersionAuthorization|^TestConsoleIAM"
            entry = {"sourcePath": str(directory), "selector": selector, "status": "running"}
            report["profiles"][name] = entry
            core_names = ["console-server-base.patch"]
            if name == "lifecycle":
                core_names.append("lifecycle-storage.patch")
            applied_patches = [
                {"name": patch_name, **report["patches"][patch_name]}
                for patch_name in core_names
            ]
            profile_extra = (storage_extra if name == "lifecycle" else []) + extra
            for path in profile_extra:
                run(f"{name} extra patch {path.name} applies", ["git", "apply", "--check", str(path)], directory)
                run(f"{name} apply extra patch {path.name}", ["git", "apply", str(path)], directory)
                applied_patches.append({"name": path.name, "path": str(path), "sha256": sha256(path.read_bytes())})
            if any((directory / file).read_bytes() != (pin / file).read_bytes() for file in ("go.mod", "go.sum")):
                raise AssertionError(f"{name} extra patches changed module files")
            source_tree = sha256(json.dumps(inventory(directory), sort_keys=True).encode())
            entry["sourceTreeSHA256"] = source_tree
            entry["nativeProfileIdentity"] = {
                "profile": name, "baseRevision": args.base,
                "appliedPatches": applied_patches, "sourceTreeSHA256": source_tree,
            }
            report["finalSourceTreeSHA256"][name] = source_tree
            if not args.skip_build:
                binary = work / f"otterio-{name}"
                run(f"{name} server build", [args.go, "build", "-o", str(binary), "."], directory, env)
                entry["binary"] = {
                    "path": str(binary), "sha256": sha256(binary.read_bytes()),
                    "profile": name, "sourceTreeSHA256": source_tree,
                    "buildInfo": json.loads(run(
                        f"{name} binary build identity",
                        [args.go, "version", "-m", "-json", str(binary)], work, env)),
                }
            if not args.skip_tests:
                flags = ["-race"] if not args.no_race else []
                run(f"{name} cmd tests", [args.go, "test", *flags, "./cmd", "-run", selector, "-count=1"], directory, env)
                if extra:
                    run(f"{name} policy package tests", [args.go, "test", *flags, "./pkg/iam/policy", "-count=1"], directory, env)
                if name == "lifecycle":
                    run("lifecycle package tests", [args.go, "test", *flags, "./pkg/bucket/lifecycle", "-count=1"], directory, env)
                    run("storage class package tests", [args.go, "test", *flags, "./cmd/config/storageclass", "-count=1"], directory, env)
            entry["status"] = "passed"
        report["status"] = "passed"
    except Exception as exc:
        report["status"] = "failed"
        report["error"] = str(exc)
        print(str(exc), file=sys.stderr, flush=True)
    finally:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
        print(f"Report: {output}", flush=True)
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
