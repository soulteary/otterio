#!/usr/bin/env python3
"""Create one new tag at the verified main workflow commit; never publish."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("preflight", ROOT / "buildscripts/release-preflight.py")
PREFLIGHT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREFLIGHT)
REQUIRED_WORKFLOWS = ('go.yml', 'lint.yml', 'release-checks.yml')


def command(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True, encoding="utf-8").strip()


def prepare(tag):
    PREFLIGHT.release_version(tag)
    repository = os.environ.get("GITHUB_REPOSITORY", "")
    if repository != "soulteary/otterio" or os.environ.get("GITHUB_REF") != "refs/heads/main":
        raise ValueError("tag preparation requires this repository's trusted main workflow")
    sha = os.environ.get("GITHUB_SHA", "")
    head = command("git", "rev-parse", "HEAD")
    if len(sha) != 40 or sha != head or command("git", "status", "--porcelain"):
        raise ValueError("release source must be the clean exact workflow commit")
    if command("git", "rev-parse", "refs/remotes/origin/main") != sha:
        raise ValueError("release source is no longer origin/main")
    if command("git", "tag", "--list", tag):
        raise ValueError("tag already exists; never move or reuse a release tag")
    if not (ROOT / "RELEASE_NOTES.md").read_text(encoding="utf-8").strip():
        raise ValueError("release notes must be reviewed before tag preparation")
    for workflow in REQUIRED_WORKFLOWS:
        payload = json.loads(command("gh", "api", f"repos/{repository}/actions/workflows/{workflow}/runs?head_sha={sha}&branch=main&event=push&per_page=1"))
        runs = payload.get("workflow_runs", [])
        if len(runs) != 1 or any(runs[0].get(k) != v for k, v in {
            "head_sha": sha, "head_branch": "main", "event": "push",
            "status": "completed", "conclusion": "success",
        }.items()):
            raise ValueError(f"{workflow} must succeed on main at the exact release source")
    branch = json.loads(command("gh", "api", f"repos/{repository}/git/ref/heads/main"))
    if branch.get("object", {}).get("sha") != sha:
        raise ValueError("main changed during validation; run preparation again")
    # GitHub rejects a duplicate ref atomically. Never PATCH an existing tag.
    command("gh", "api", "--method", "POST", f"repos/{repository}/git/refs", "-f", f"ref=refs/tags/{tag}", "-f", f"sha={sha}")
    # GITHUB_TOKEN-created tags do not trigger push workflows. Explicitly run
    # the draft builder from the immutable tag's workflow instead.
    command("gh", "workflow", "run", "release.yml", "--repo", repository, "--ref", tag, "-f", f"tag={tag}")
    print(f"Created {tag} at {sha}; dispatched verified draft build. No release was published.")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 2:
            raise ValueError("provide exactly one new release tag")
        prepare(sys.argv[1])
    except (ValueError, OSError, subprocess.CalledProcessError, PREFLIGHT.PreflightError) as exc:
        print(f"release tag preparation failed: {exc}", file=sys.stderr)
        sys.exit(1)
