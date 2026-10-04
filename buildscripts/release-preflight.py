#!/usr/bin/env python3
"""Read-only release validation; requires only Python 3 and Git.

Fetch origin/main and tags before running locally. This script never fetches,
creates/pushes a tag, publishes artifacts, or claims that remote CI has passed.
"""

import argparse
from datetime import datetime
from pathlib import Path
import re
import subprocess
import sys


class PreflightError(Exception):
    """A release prerequisite is not satisfied."""


def release_version(tag: str) -> str:
    if not re.fullmatch(
        r"RELEASE\.[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}Z", tag
    ):
        raise PreflightError("use RELEASE.YYYY-MM-DDTHH-MM-SSZ (UTC), not SemVer")
    try:
        stamp = datetime.strptime(tag, "RELEASE.%Y-%m-%dT%H-%M-%SZ")
    except ValueError as exc:
        raise PreflightError("release tag contains an invalid UTC date/time") from exc
    return stamp.isoformat(timespec="seconds") + "Z"


def git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=root, text=True, capture_output=True, check=False
    )
    if result.returncode:
        raise PreflightError(result.stderr.strip() or "git command failed")
    return result.stdout.strip()


def check_release(tag: str, root: Path, existing: bool = False) -> dict:
    version = release_version(tag)
    root = Path(git(root, "rev-parse", "--show-toplevel"))
    if git(root, "status", "--porcelain", "--untracked-files=all"):
        raise PreflightError("working tree is dirty; commit/stash changes first")
    head = git(root, "rev-parse", "--verify", "HEAD^{commit}")
    main = git(root, "rev-parse", "--verify", "refs/remotes/origin/main^{commit}")
    if existing:
        tagged = git(root, "rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}")
        if tagged != head:
            raise PreflightError("checked-out HEAD does not match the requested tag")
        git(root, "merge-base", "--is-ancestor", head, main)
    else:
        branch = git(root, "symbolic-ref", "--quiet", "--short", "HEAD")
        if branch != "main" or head != main:
            raise PreflightError("release from main at the freshly fetched origin/main")
        tags = git(root, "tag", "--list", tag)
        if tags:
            raise PreflightError("tag already exists; never move or reuse a release tag")
    notes = root / "RELEASE_NOTES.md"
    if not notes.is_file() or not notes.read_text(encoding="utf-8").strip():
        raise PreflightError("RELEASE_NOTES.md must exist and contain reviewed notes")
    return {"tag": tag, "version": version, "sha": head}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag", help="RELEASE.YYYY-MM-DDTHH-MM-SSZ")
    parser.add_argument("--existing", action="store_true", help="validate a checked-out tag in CI")
    args = parser.parse_args()
    try:
        metadata = check_release(args.tag, Path.cwd(), args.existing)
    except (PreflightError, OSError, UnicodeError) as exc:
        print(f"release preflight failed: {exc}", file=sys.stderr)
        return 1
    # Strict tag/date validation and Git-generated SHAs make these safe for
    # GITHUB_OUTPUT. Keep stdout machine-readable and diagnostics on stderr.
    for key, value in metadata.items():
        print(f"{key}={value}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
