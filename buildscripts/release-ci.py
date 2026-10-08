#!/usr/bin/env python3
"""Wait for required main push CI on an exact release commit, read-only."""

import argparse
import json
import re
import subprocess
import sys
import time
from urllib.parse import urlencode


WORKFLOWS = ("go.yml", "lint.yml", "release-checks.yml")
WAITING_STATUSES = {"queued", "requested", "waiting", "pending", "in_progress"}


class CIError(Exception):
    """Required release CI could not be verified."""


def validate_identity(repository, sha):
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository) or any(
        part in {".", ".."} for part in repository.split("/")
    ):
        raise CIError("repository must use the owner/repository format")
    if not re.fullmatch(r"[0-9a-fA-F]{40}", sha):
        raise CIError("release SHA must contain exactly 40 hexadecimal characters")


def fetch_run(repository, sha, workflow):
    """Return the latest matching main push run, or None if not visible yet."""
    validate_identity(repository, sha)
    if workflow not in WORKFLOWS:
        raise CIError(f"unsupported required workflow: {workflow}")
    query = urlencode(dict(head_sha=sha, branch="main", event="push", per_page=1))
    try:
        response = subprocess.run(
            [
                "gh", "api", "--method", "GET",
                f"repos/{repository}/actions/workflows/{workflow}/runs?{query}",
            ],
            check=False, capture_output=True, text=True, timeout=30,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise CIError(f"{workflow}: CI API request failed: {exc}") from exc
    if response.returncode:
        detail = response.stderr.strip() or f"gh exited with status {response.returncode}"
        raise CIError(f"{workflow}: CI API request failed: {detail}")
    try:
        payload = json.loads(response.stdout)
    except (ValueError, TypeError) as exc:
        raise CIError(f"{workflow}: CI API returned invalid JSON") from exc
    if not isinstance(payload, dict) or not isinstance(payload.get("workflow_runs"), list):
        raise CIError(f"{workflow}: CI API returned an invalid workflow run list")
    runs = payload["workflow_runs"]
    if not runs:
        return None
    if len(runs) != 1 or not isinstance(runs[0], dict):
        raise CIError(f"{workflow}: CI API returned an invalid workflow run")
    run = runs[0]
    link = run.get("html_url")
    context = f"{workflow} ({link})" if isinstance(link, str) and link else workflow
    if (
        run.get("head_sha") != sha
        or run.get("head_branch") != "main"
        or run.get("event") != "push"
    ):
        raise CIError(f"{context}: CI API returned a run for a different source or event")
    if (
        not isinstance(run.get("status"), str)
        or not run["status"]
        or "conclusion" not in run
        or (run["conclusion"] is not None and not isinstance(run["conclusion"], str))
        or not isinstance(link, str)
        or not link.startswith("https://")
    ):
        raise CIError(f"{context}: CI API returned malformed run metadata")
    return run


def wait_for_ci(repository, sha, timeout=3600, interval=30):
    """Pass only after every required workflow succeeds before publication."""
    validate_identity(repository, sha)
    if type(timeout) is not int or timeout <= 0:
        raise CIError("timeout must be a positive integer")
    if type(interval) is not int or interval <= 0:
        raise CIError("interval must be a positive integer")
    sha = sha.lower()
    deadline = time.monotonic() + timeout
    detail = ""
    while True:
        if detail and time.monotonic() >= deadline:
            raise CIError(f"Timed out waiting for main CI at {sha}: {detail}")
        waiting = []
        for workflow in WORKFLOWS:
            run = fetch_run(repository, sha, workflow)
            if run is None:
                waiting.append(
                    f"{workflow}: run not visible yet "
                    f"(https://github.com/{repository}/actions/workflows/{workflow})"
                )
                continue
            status = run["status"]
            link = run["html_url"]
            if status == "completed":
                if run["conclusion"] != "success":
                    raise CIError(
                        f"{workflow}: main CI at {sha} completed with "
                        f"{run['conclusion']!r}; {link}"
                    )
            elif status in WAITING_STATUSES:
                waiting.append(f"{workflow}: {status} ({link})")
            else:
                raise CIError(f"{workflow}: unexpected CI status {status!r}; {link}")
        detail = "; ".join(waiting)
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise CIError(f"Timed out verifying main CI at {sha}: {detail or 'API verification exceeded the deadline'}")
        if not waiting:
            print(f"Required main CI succeeded for {sha}.", flush=True)
            return
        print(f"Waiting for main CI at {sha}: {detail}", flush=True)
        time.sleep(min(interval, remaining))


class CIArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        raise CIError(message)


def main():
    parser = CIArgumentParser(description=__doc__)
    parser.add_argument("repository", help="GitHub owner/repository")
    parser.add_argument("sha", help="exact 40-character release source SHA")
    parser.add_argument("--timeout", type=int, default=3600, help="maximum wait in seconds")
    parser.add_argument("--interval", type=int, default=30, help="poll interval in seconds")
    try:
        args = parser.parse_args()
        wait_for_ci(args.repository, args.sha, args.timeout, args.interval)
    except CIError as exc:
        message = str(exc).replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")
        print(f"::error::{message}", file=sys.stderr, flush=True)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
