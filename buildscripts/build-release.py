#!/usr/bin/env python3
"""Build the six release binaries locally without creating tags or publishing."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
TARGETS = (
    ("linux", "amd64"), ("linux", "arm64"), ("linux", "ppc64le"),
    ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64"),
)
SPEC = importlib.util.spec_from_file_location("release_preflight", ROOT / "buildscripts/release-preflight.py")
preflight = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(preflight)


def git(source, *args):
    return subprocess.run(["git", *args], cwd=source, check=True,
                          capture_output=True, text=True).stdout.strip()


def clean_source(source):
    if Path(git(source, "rev-parse", "--show-toplevel")).resolve() != source:
        raise ValueError("source must be the Git repository root")
    if git(source, "status", "--porcelain", "--untracked-files=all"):
        raise ValueError("source worktree must be clean before and after building")
    return git(source, "rev-parse", "--verify", "HEAD^{commit}")


def build_release(tag, output, source=ROOT):
    version = preflight.release_version(tag)
    source, output = Path(source).resolve(), Path(output).resolve()
    if output == source or source in output.parents:
        raise ValueError("output must be outside the source repository to preserve clean VCS metadata")
    if output.exists() and (not output.is_dir() or any(output.iterdir())):
        raise ValueError("output must be a new or empty directory")
    sha = clean_source(source)
    env = dict(os.environ, CGO_ENABLED="0", GO111MODULE="on",
               OTTERIO_RELEASE="RELEASE", OTTERIO_BUILD_COMMIT=sha)
    for key in ("GOOS", "GOARCH", "OTTERIO_HOTFIX"):
        env.pop(key, None)
    ldflags = subprocess.run(
        ["go", "run", "buildscripts/gen-ldflags.go", version],
        cwd=source, env=env, check=True, capture_output=True, text=True,
    ).stdout.strip()
    if not ldflags:
        raise ValueError("release linker flags are empty")
    output.mkdir(parents=True, exist_ok=True)
    assets = []
    for goos, goarch in TARGETS:
        name = f"otterio-{goos}-{goarch}" + (".exe" if goos == "windows" else "")
        target = output / name
        subprocess.run(
            ["go", "build", "-tags", "kqueue", "-trimpath", "-ldflags", ldflags,
             "-o", str(target), "."],
            cwd=source, env=dict(env, GOOS=goos, GOARCH=goarch), check=True,
        )
        assets.append(target)
    if clean_source(source) != sha:
        raise ValueError("source commit changed while building")
    # This binary-only placeholder deliberately fails image promotion validation.
    # The draft workflow replaces it with the actual, smoke-tested image digests.
    manifest = {"schema_version": 1, "tag": tag, "source_sha": sha, "images": []}
    (output / "release-manifest.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8",
    )
    assets.append(output / "release-manifest.json")
    checksums = "".join(
        hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name + "\n"
        for path in sorted(assets)
    )
    (output / f"otterio-{tag}-checksums.txt").write_text(checksums, encoding="utf-8")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag", help="RELEASE.YYYY-MM-DDTHH-MM-SSZ")
    parser.add_argument("--output", required=True, type=Path, help="new or empty directory outside source")
    parser.add_argument("--source-dir", type=Path, default=ROOT, help="clean Git source checkout")
    args = parser.parse_args()
    try:
        build_release(args.tag, args.output, args.source_dir)
    except (ValueError, OSError, subprocess.CalledProcessError, preflight.PreflightError) as exc:
        print(f"release build failed: {exc}", file=sys.stderr)
        return 1
    print(f"Built release binaries and checksums in {args.output.resolve()}; no publication performed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
