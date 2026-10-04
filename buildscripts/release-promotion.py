#!/usr/bin/env python3
"""Create validated release manifests and plan read-only digest promotion."""
import argparse
from datetime import datetime
import json
from pathlib import Path
import re
import sys


def release_time(tag):
    if not isinstance(tag, str) or not re.fullmatch(r"RELEASE\.[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}Z", tag):
        raise ValueError("invalid release tag")
    return datetime.strptime(tag, "RELEASE.%Y-%m-%dT%H-%M-%SZ")


def image_repositories(repository, dockerhub_user=""):
    """Derive public identities locally instead of transporting secret-like outputs."""
    if not isinstance(repository, str) or not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("invalid repository")
    names = ["ghcr.io/" + repository.lower()]
    if dockerhub_user:
        if not isinstance(dockerhub_user, str) or not re.fullmatch(r"[a-z0-9_-]+", dockerhub_user):
            raise ValueError("invalid Docker Hub username")
        names.append(dockerhub_user + "/otterio")
    return names


def validate_manifest(tag, manifest, source_sha, repository, dockerhub_user=""):
    """Apply the same identity checks before publication and before promotion."""
    release_time(tag)
    if not isinstance(source_sha, str) or not re.fullmatch(r"[0-9a-f]{40}", source_sha):
        raise ValueError("invalid source SHA")
    expected = image_repositories(repository, dockerhub_user)
    if not isinstance(manifest, dict) or type(manifest.get("schema_version")) is not int or manifest["schema_version"] != 1:
        raise ValueError("unsupported release manifest")
    if manifest.get("tag") != tag or manifest.get("source_sha") != source_sha:
        raise ValueError("release manifest does not match the current tag commit")
    images = manifest.get("images")
    if not isinstance(images, list) or not images or len(images) > 2:
        raise ValueError("release manifest must contain one or two images")
    ghcr = expected[0]
    allowed = set(expected)
    seen = set()
    for image in images:
        if not isinstance(image, dict):
            raise ValueError("invalid image entry")
        name, digest = image.get("repository"), image.get("digest")
        if not isinstance(name, str) or name not in allowed or name in seen:
            raise ValueError("unexpected or duplicate image repository")
        if not isinstance(digest, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise ValueError("invalid image digest")
        seen.add(name)
    if ghcr not in seen:
        raise ValueError("release manifest has no primary GHCR image")
    return images


def build_manifest(tag, source_sha, repository, digest, dockerhub_published, dockerhub_user=""):
    """Record only registries selected by the successful image build job."""
    # Missing job outputs must fail closed, not silently omit a published image.
    if dockerhub_published not in ("true", "false"):
        raise ValueError("missing or invalid Docker Hub publication status")
    if dockerhub_published == "true" and not dockerhub_user:
        raise ValueError("Docker Hub username is required for a published image")
    selected_user = dockerhub_user if dockerhub_published == "true" else ""
    images = [
        {"repository": name, "digest": digest}
        for name in image_repositories(repository, selected_user)
    ]
    manifest = {"schema_version": 1, "tag": tag, "source_sha": source_sha, "images": images}
    validate_manifest(tag, manifest, source_sha, repository, selected_user)
    return manifest


def promotion_plan(tag, manifest, pages, source_sha, repository, dockerhub_user=""):
    candidate = release_time(tag)
    images = validate_manifest(tag, manifest, source_sha, repository, dockerhub_user)
    if not isinstance(pages, list):
        raise ValueError("invalid published release listing")
    releases = []
    for page in pages:
        releases.extend(page if isinstance(page, list) else [page])
    stable = {}
    for release in releases:
        if not isinstance(release, dict):
            raise ValueError("invalid published release entry")
        if release.get("draft") is not False or release.get("prerelease") is not False:
            continue
        published_tag = release.get("tag_name")
        try:
            stamp = release_time(published_tag)
        except (TypeError, ValueError):
            continue  # Historical releases outside this timestamp scheme.
        stable[published_tag] = stamp
    if tag not in stable:
        raise ValueError("candidate is not a published stable release")
    newest = max(stable, key=stable.get)
    promote = candidate >= stable[newest]
    return {
        "tag": tag, "source_sha": source_sha, "images": images,
        "promote": promote, "has_dockerhub": len(images) == 2,
        "reason": "newest stable release" if promote else "newer stable release exists: " + newest,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--validate-tag", action="store_true")
    mode.add_argument("--write-manifest", type=Path)
    parser.add_argument("--digest")
    parser.add_argument("--dockerhub-published", choices=("true", "false"))
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--published", type=Path)
    parser.add_argument("--source-sha")
    parser.add_argument("--repository")
    parser.add_argument("--dockerhub-user", default="")
    args = parser.parse_args()
    try:
        release_time(args.tag)
        if args.validate_tag:
            return 0
        if args.write_manifest:
            manifest = build_manifest(
                args.tag, args.source_sha, args.repository, args.digest,
                args.dockerhub_published, args.dockerhub_user,
            )
            # Do not open/truncate the destination until every field is valid.
            args.write_manifest.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
            return 0
        if not all((args.manifest, args.published, args.source_sha, args.repository)):
            raise ValueError("manifest, published listing, source SHA and repository are required")
        plan = promotion_plan(
            args.tag, json.loads(args.manifest.read_text()),
            json.loads(args.published.read_text()), args.source_sha,
            args.repository, args.dockerhub_user,
        )
        print(json.dumps(plan, sort_keys=True))
    except (OSError, TypeError, ValueError) as exc:
        operation = "manifest" if args.write_manifest else "promotion"
        print("release " + operation + " refused: " + str(exc), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
