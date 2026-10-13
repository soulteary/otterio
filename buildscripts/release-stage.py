#!/usr/bin/env python3
"""Bind candidate bytes to acceptance; publish an existing draft without rebuilding."""
import argparse
import base64
import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import tarfile
import tempfile
import zipfile

REPOSITORIES = ('soulteary/oc', 'soulteary/otterio')
ACCEPTANCE_REPOSITORY = 'soulteary/oc'
ACCEPTANCE_WORKFLOW = '.github/workflows/console-release-acceptance.yml'
PROFILES = ['storage', 'settings', 'features-version-copy', 'native',
            'relationship-iam', 'real-protocol', 'oidc-browser-single', 'oidc-browser-dual']
OC_TARGETS = ['linux-amd64', 'linux-ppc64le', 'linux-arm64', 'linux-s390x',
              'darwin-amd64', 'darwin-arm64', 'freebsd-amd64', 'linux-arm',
              'linux-386', 'windows-amd64', 'windows-arm64']
OT_TARGETS = ['linux-amd64', 'linux-arm64', 'linux-ppc64le',
              'darwin-amd64', 'darwin-arm64', 'windows-amd64']
PRESERVED_STATE = ': objects versions tags bucket configuration IAM group policy denies and CLI config preserved'
UPGRADE_CHECKS = [
    'previous-baseline' + PRESERVED_STATE,
    'previous published console writes shared local preferences',
    'candidate accepts omitted browser environment with retained TLS S3/Admin listeners',
    'candidate-without-new-writes' + PRESERVED_STATE,
    'old-to-new local preferences preserved; native identity isolated and persistent after restart',
    'candidate legacy routes unavailable while verified S3 and Admin remain reachable',
    'rollback-without-new-writes' + PRESERVED_STATE,
    'previous console can read local preferences after current console',
    'candidate accepts explicit browser off with retained TLS S3/Admin listeners',
    'candidate-after-new-write' + PRESERVED_STATE,
    'rollback-after-new-write' + PRESERVED_STATE,
    'snapshot-restored-baseline' + PRESERVED_STATE,
    'pre-upgrade snapshot recovery omits subsequent writes as expected',
]


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha256(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def tag_valid(tag):
    require(isinstance(tag, str) and re.fullmatch(
        r'RELEASE\.[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}Z', tag), 'invalid timestamp tag')
    datetime.datetime.strptime(tag, 'RELEASE.%Y-%m-%dT%H-%M-%SZ')


def hash_valid(value):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) is not None


def asset_names(repository, tag):
    tag_valid(tag)
    require(repository in REPOSITORIES, 'unsupported repository')
    if repository.endswith('/oc'):
        return {f'oc-{tag}-{target}.' + ('zip' if target.startswith('windows') else 'tar.gz')
                for target in OC_TARGETS} | {'checksums.txt', 'release-manifest.json'}
    return {'otterio-' + target + ('.exe' if target.startswith('windows') else '')
            for target in OT_TARGETS} | {f'otterio-{tag}-checksums.txt', 'release-manifest.json'}


def inventory(directory, repository, tag, require_images=True):
    """No symlinks, extra files, duplicate checksums, or partially checked assets."""
    directory = Path(directory)
    expected = asset_names(repository, tag)
    files = list(directory.iterdir())
    require({p.name for p in files} == expected and
            all(p.is_file() and not p.is_symlink() for p in files), 'incomplete or unexpected candidate assets')
    checksum = 'checksums.txt' if repository.endswith('/oc') else f'otterio-{tag}-checksums.txt'
    values = {}
    for line in (directory / checksum).read_text().splitlines():
        parts = line.split(maxsplit=1)
        require(len(parts) == 2 and hash_valid(parts[0]), 'invalid checksum line')
        name = parts[1].lstrip('*')
        require(name in expected and name != checksum and name not in values, 'unsafe or duplicate checksum name')
        values[name] = parts[0]
    require(set(values) == expected - {checksum}, 'checksum does not cover every candidate asset')
    actual = {name: sha256(directory / name) for name in sorted(expected)}
    require(all(actual[name] == value for name, value in values.items()), 'candidate checksum mismatch')
    manifest = json.loads((directory / 'release-manifest.json').read_text())
    oc = repository.endswith('/oc')
    source = manifest.get('source_commit' if oc else 'source_sha')
    require(type(manifest.get('schema_version')) is int and manifest['schema_version'] == 1 and
            manifest.get('release_tag' if oc else 'tag') == tag and
            isinstance(source, str) and re.fullmatch('[0-9a-f]{40}', source), 'candidate manifest identity mismatch')
    if oc:
        rows = manifest.get('assets', [])
        require(len(rows) == len(OC_TARGETS) and {r.get('name') for r in rows} == expected - {checksum, 'release-manifest.json'} and
                {r.get('target') for r in rows} == {t.replace('-', '/', 1) for t in OC_TARGETS} and
                all(r.get('sha256') == actual.get(r.get('name')) and r.get('name') ==
                    f'oc-{tag}-' + r['target'].replace('/', '-') + ('.zip' if r['target'].startswith('windows/') else '.tar.gz')
                    for r in rows), 'archive manifest mismatch')
    images = manifest.get('images', [])
    require(isinstance(images, list) and len(images) <= 2 and (not require_images or images), 'candidate image identity missing')
    seen = set()
    for row in images:
        name, digest = row.get('repository'), row.get('digest')
        require(isinstance(name, str) and (name == 'ghcr.io/' + repository or
                re.fullmatch(r'[a-z0-9][a-z0-9_-]*/' + repository.split('/')[1], name)) and
                name not in seen and isinstance(digest, str) and re.fullmatch(r'sha256:[0-9a-f]{64}', digest), 'invalid candidate image')
        seen.add(name)
    require(not require_images or 'ghcr.io/' + repository in seen, 'primary image identity missing')
    if images:
        require(images[0]['repository'] == 'ghcr.io/' + repository and
                all(row['digest'] == images[0]['digest'] for row in images),
                'registry mirror differs from the exercised primary image')
    return {'repository': repository, 'tag': tag, 'sourceSHA': source, 'assets': actual, 'images': images}


def command(args, binary=False, write=False):
    env = dict(os.environ)
    reader = env.pop('CONSOLE_RELEASE_TOKEN', '')
    if args[0] == 'gh' and not write and reader:
        env['GH_TOKEN'] = reader
    if args[0] != 'gh':
        env.pop('GH_TOKEN', None)
        env.pop('GITHUB_TOKEN', None)
    result = subprocess.run(args, capture_output=True, text=not binary, check=False, env=env)
    require(result.returncode == 0, 'external verification command failed: ' + args[0])
    return result.stdout


def api(path):
    return json.loads(command(['gh', 'api', path]))


def release_metadata(repository, tag, draft=True):
    """Find a unique draft by collection/ID; GitHub's tag endpoint is published-only."""
    tag_valid(tag)
    require(repository in REPOSITORIES, 'unsupported repository')
    if draft is False:
        release = api(f'repos/{repository}/releases/tags/{tag}')
    else:
        matches = []
        page = 1
        while True:
            rows = api(f'repos/{repository}/releases?per_page=100&page={page}')
            require(isinstance(rows, list) and len(rows) <= 100 and
                    all(isinstance(row, dict) for row in rows), 'invalid release listing')
            matches.extend(row for row in rows if row.get('tag_name') == tag)
            require(len(matches) <= 1, 'expected one official release for tag')
            if len(rows) < 100:
                break
            page += 1
        require(len(matches) == 1, 'expected one official release for tag')
        selected = matches[0]
        release_id = selected.get('id')
        require(type(release_id) is int and release_id > 0, 'invalid official release ID')
        require(selected.get('prerelease') is False and type(selected.get('draft')) is bool and
                (draft is None or selected['draft'] is draft), 'unexpected release publication state')
        release = api(f'repos/{repository}/releases/{release_id}')
        require(isinstance(release, dict) and type(release.get('id')) is int and
                all(release.get(field) == selected.get(field) for field in
                    ('id', 'tag_name', 'draft', 'prerelease')), 'release identity changed during lookup')
    require(isinstance(release, dict) and release.get('tag_name') == tag and
            release.get('prerelease') is False and type(release.get('draft')) is bool and
            (draft is None or release.get('draft') is draft), 'unexpected release publication state')
    return release


def snapshot(repository, tag, destination, draft=True):
    """Download every official asset and verify metadata digests independently."""
    release = release_metadata(repository, tag, draft)
    names = asset_names(repository, tag)
    assets = release.get('assets', [])
    require(len(assets) == len(names) and {r.get('name') for r in assets} == names, 'unexpected official release assets')
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=False)
    command(['gh', 'release', 'download', tag, '--repo', repository, '--dir', str(destination)])
    result = inventory(destination, repository, tag)
    for row in assets:
        require(row.get('state') == 'uploaded' and row.get('digest') == 'sha256:' + result['assets'][row['name']],
                'official asset digest mismatch')
    # Resolve annotated as well as lightweight tags independently of the manifest.
    ref = api(f'repos/{repository}/git/ref/tags/{tag}')['object']
    seen = set()
    while ref.get('type') == 'tag':
        require(ref['sha'] not in seen, 'tag cycle')
        seen.add(ref['sha'])
        ref = api(f'repos/{repository}/git/tags/{ref["sha"]}')['object']
    require(ref.get('type') == 'commit' and ref.get('sha') == result['sourceSHA'], 'remote tag differs from candidate source')
    result.update(releaseID=release['id'], releaseNotesSHA256=hashlib.sha256((release.get('body') or '').encode()).hexdigest())
    return result


def verify_images(release):
    images = release.get('images', [])
    require(images and all(row.get('digest') == images[0].get('digest') for row in images),
            'registry mirror differs from the exercised primary image')
    for row in release['images']:
        actual = json.loads(command(['docker', 'buildx', 'imagetools', 'inspect',
                                    row['repository'] + ':' + release['tag'], '--format', '{{json .Manifest}}']))
        require(actual.get('digest') == row['digest'], 'candidate image version tag changed')


def packaged_hashes(directory, repository, tag):
    """Publication checks actual archived program bytes, not report labels."""
    directory = Path(directory)
    if repository == 'soulteary/otterio':
        return {'server': sha256(directory / 'otterio-linux-amd64')}
    result = {}
    stem = f'oc-{tag}-linux-amd64'
    with tarfile.open(directory / (stem + '.tar.gz'), 'r:gz') as data:
        rows = data.getmembers()
        for key, name in [('oc', 'oc'), ('console', 'oc-console')]:
            found = [r for r in rows if r.name == stem + '/' + name]
            require(len(found) == 1 and found[0].isfile() and 0 < found[0].size < 256_000_000, 'unsafe or missing packaged program')
            with data.extractfile(found[0]) as stream:
                result[key] = hashlib.file_digest(stream, 'sha256').hexdigest()
    return result


def extract_attestation(archive, destination):
    with zipfile.ZipFile(archive) as data:
        rows = data.infolist()
        names = [r.filename for r in rows]
        require(len(names) == len(set(names)) and sum(r.file_size for r in rows) < 50_000_000, 'unsafe attestation archive')
        for row in rows:
            p = PurePosixPath(row.filename)
            require(not p.is_absolute() and '..' not in p.parts and len(p.parts) == 1 and row.filename == p.as_posix() and '\\' not in row.filename and
                    row.filename.endswith('.json') and not stat.S_ISLNK(row.external_attr >> 16), 'unsafe attestation path')
            (Path(destination) / row.filename).write_bytes(data.read(row))


def load_pair_validator():
    # Only sibling code reviewed with this dispatched publication commit executes.
    path = Path(__file__).with_name('console_pair_evidence.py')
    spec = importlib.util.spec_from_file_location('local_pair_validator', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def validate_evidence(document, directory, validator=None):
    require(type(document.get('schemaVersion')) is int and document['schemaVersion'] == 1 and document.get('evidenceKind') == 'draft-release-pair' and
            document.get('passed') is True and document.get('publishable') is True, 'nonpublishable acceptance evidence')
    require(set(document.get('releases', {})) == set(REPOSITORIES), 'acceptance pair missing')
    reports = document.get('reportFiles', {})
    require(isinstance(reports, dict) and 'pair.json' in reports and 'upgrade.json' in reports and 'images.json' in reports,
            'acceptance raw reports missing')
    for name, digest in reports.items():
        require(PurePosixPath(name).name == name and name.endswith('.json') and hash_valid(digest) and
                (Path(directory) / name).is_file() and sha256(Path(directory) / name) == digest, 'acceptance report digest mismatch')
    require({p.name for p in Path(directory).glob('*.json')} == set(reports) | {'acceptance.json'}, 'unexpected evidence report inventory')
    pair = json.loads((Path(directory) / 'pair.json').read_text())
    require(pair.get('passed') is True and pair.get('requiredProfiles') == PROFILES and
            [r.get('name') for r in pair.get('checks', [])] == PROFILES and
            all(r.get('status') == 'passed' and r.get('exitCode') == 0 for r in pair['checks']), 'required profile not passed')
    for row in pair['checks']:
        require(reports.get(row.get('report')) == row.get('reportSHA256'), 'original child report not bound')
        for extra in row.get('additionalReports', []):
            require(reports.get(extra.get('report')) == extra.get('reportSHA256'), 'additional child report not bound')
    validator = validator or load_pair_validator()
    for row in pair['checks']:
        child = json.loads((Path(directory) / row['report']).read_text())
        if row['name'] == 'real-protocol':
            validator.validate_protocol(child, pair['programs'])
            extras = row.get('additionalReports', [])
            require(len(extras) == 1, 'original Range report missing')
            raw = json.loads((Path(directory) / extras[0]['report']).read_text())
            range_check = next(r for r in child['checks'] if r['name'] == 'range')
            require(raw.get('serverSource') == pair['programs']['server']['source'] and
                    raw.get('profiles') == range_check.get('profiles'), 'original Range report differs from protocol evidence')
            for result in raw['profiles']:
                require(result.get('checks') == 9 and result.get('emptyObjectChecks') == 2 and
                        isinstance(result.get('fixtureBytes'), int) and result['fixtureBytes'] > 0 and
                        hash_valid(result.get('sha256')) and result.get('nullVersionStatus') in (206, 501) and
                        result.get('historicalVersionStatus') == 206 and result.get('restrictedCurrentStatus') == 206 and
                        result.get('restrictedHistoricalStatus') == 403 and result.get('restrictedConditionalFullStatus') == 200 and
                        result.get('restrictedIfRangeFallbackStatus') == 200, 'original Range check omitted or failed')
        elif row['name'].startswith('oidc-browser-'):
            validator.validate_oidc(child, row['name'].endswith('-dual'), pair['programs'])
        else:
            validator.validate_integration(child, row['name'], pair['programs'])
    upgrade = json.loads((Path(directory) / 'upgrade.json').read_text())
    require(upgrade.get('passed') is True and {p.get('profile') for p in upgrade.get('profiles', [])} == {'single-tls', 'dual-tls'} and
            len(upgrade['profiles']) == 2 and all(p.get('status') == 'passed' and p.get('cleanup') == 'passed'
                                              for p in upgrade['profiles']), 'upgrade or cleanup not passed')
    for profile in upgrade['profiles']:
        checks = profile.get('checks', [])
        require(len(checks) == len(UPGRADE_CHECKS) and all(isinstance(row, dict) and row.get('status') == 'passed'
                for row in checks) and [row.get('name') for row in checks] == UPGRADE_CHECKS,
                'upgrade check omitted or skipped')
    previous = upgrade.get('previousReleaseManifests', {})
    require(previous.get('server', {}).get('tag') == 'RELEASE.2026-10-10T16-38-05Z' and
            previous['server'].get('source') == '8cc518b31ac8b69c8f855e19b4bd027f44275c47' and
            previous['server'].get('expectedManifestSHA256') == '2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3' and
            previous['server'].get('manifestSHA256') == previous['server']['expectedManifestSHA256'] and
            previous['server'].get('verification') == 'matches-expected-sha256' and
            previous.get('oc', {}).get('tag') == 'RELEASE.2026-10-10T11-42-18Z' and
            previous['oc'].get('source') == 'ee1846d587a6548db4e595d653ca4f54baea2b1a', 'previous release provenance mismatch')
    old = {key: upgrade.get('programs', {}).get(key) for key in ('previousServer', 'previousCLI', 'previousConsole')}
    require(old == document.get('previousPrograms') and
            all(isinstance(row, dict) and hash_valid(row.get('binarySHA256')) and
                row.get('source') == previous['server' if key == 'previousServer' else 'oc']['source'] for key, row in old.items()),
            'previous program tuple mismatch')
    programs = pair.get('programs', {})
    releases = document['releases']
    for release in releases.values():
        images = release.get('images', [])
        require(images and all(row.get('digest') == images[0].get('digest') for row in images),
                'registry mirror differs from the exercised primary image')
    for key, repository in [('oc', 'soulteary/oc'), ('console', 'soulteary/oc'), ('server', 'soulteary/otterio')]:
        row = programs.get(key, {})
        require(row.get('source') == releases[repository].get('sourceSHA') and row.get('binaryVCSModified') is False and
                row.get('binaryVCSRevision') == row.get('source') and row.get('platform') == 'linux/amd64' and
                row.get('binarySHA256') == document.get('packagedPrograms', {}).get(key), 'packaged program evidence mismatch')
        upgrade_key = {'oc': 'cli', 'console': 'console', 'server': 'server'}[key]
        identity = upgrade.get('programs', {}).get(upgrade_key, {})
        require(identity.get('binarySHA256') == row['binarySHA256'] and identity.get('source') == row['source'] and
                identity.get('embeddedVCS') == row['source'] and identity.get('embeddedModified') == 'false' and
                identity.get('goos') == 'linux' and identity.get('goarch') == 'amd64', 'upgrade program differs from paired artifact')
    images = json.loads((Path(directory) / 'images.json').read_text())
    require(images.get('passed') is True and images.get('mode') == 'registry-digest', 'registry image acceptance missing')
    rows = images.get('checks', [])
    require(len(rows) == 4 and {(r.get('repository'), r.get('platform')) for r in rows} ==
            {(repo, arch) for repo in REPOSITORIES for arch in ('linux/amd64', 'linux/arm64')} and
            all(r.get('passed') is True and r.get('binaryBytesMatched') is True and r.get('runtimePassed') is True and
                r.get('cleanup', {}).get('status') == 'passed' and r['cleanup'].get('errors') == [] and
                r.get('digest') == releases[r['repository']]['images'][0]['digest'] for r in rows), 'image architecture or runtime not accepted')


def verified_attestation(run_id, expected, directory):
    require(str(run_id).isdigit() and int(run_id) > 0 and hash_valid(expected), 'invalid acceptance identifier or digest')
    run = api(f'repos/{ACCEPTANCE_REPOSITORY}/actions/runs/{run_id}')
    require(run.get('status') == 'completed' and run.get('conclusion') == 'success' and
            run.get('event') == 'workflow_dispatch' and run.get('head_branch') == 'main' and
            run.get('path') == ACCEPTANCE_WORKFLOW and run.get('repository', {}).get('full_name') == ACCEPTANCE_REPOSITORY,
            'acceptance is not a successful trusted main workflow')
    listing = api(f'repos/{ACCEPTANCE_REPOSITORY}/actions/runs/{run_id}/artifacts?per_page=100')
    rows = [r for r in listing.get('artifacts', []) if r.get('name') == 'console-release-acceptance']
    require(len(rows) == 1 and rows[0].get('expired') is False and
            re.fullmatch(r'sha256:[0-9a-f]{64}', rows[0].get('digest', '')), 'official acceptance artifact missing or expired')
    row = rows[0]
    archive = Path(directory) / 'official-artifact.zip'
    archive.write_bytes(command(['gh', 'api', f'repos/{ACCEPTANCE_REPOSITORY}/actions/artifacts/{row["id"]}/zip'], binary=True))
    require('sha256:' + sha256(archive) == row['digest'], 'official acceptance archive digest mismatch')
    extract_attestation(archive, directory)
    path = Path(directory) / 'acceptance.json'
    require(path.is_file() and sha256(path) == expected, 'acceptance expectation mismatch')
    doc = json.loads(path.read_text())
    provenance = doc.get('provenance', {})
    require(provenance == {'repository': ACCEPTANCE_REPOSITORY, 'workflowPath': ACCEPTANCE_WORKFLOW,
                          'runID': int(run_id), 'runAttempt': run['run_attempt'], 'headSHA': run['head_sha'],
                          'event': 'workflow_dispatch', 'ref': 'refs/heads/main'}, 'acceptance workflow identity mismatch')
    pair = json.loads((Path(directory) / 'pair.json').read_text())
    for filename in ('buildscripts/test-console-migration-pair.py', 'buildscripts/console_pair_evidence.py'):
        source = api(f'repos/{ACCEPTANCE_REPOSITORY}/contents/{filename}?ref={run["head_sha"]}')
        require(source.get('encoding') == 'base64', 'trusted verifier source missing')
        digest = hashlib.sha256(base64.b64decode(source['content'], validate=False)).hexdigest()
        require(pair.get('harnesses', {}).get(filename) == digest, 'acceptance verifier does not match trusted workflow source')
        if filename.endswith('/console_pair_evidence.py'):
            require(digest == sha256(Path(__file__).with_name('console_pair_evidence.py')),
                    'acceptance schema differs from locally reviewed validator')
    # Downloaded source is fingerprinted as data; it is never imported or executed.
    validate_evidence(doc, directory)
    return doc


def publish(repository, tag, run_id, expected):
    require(os.environ.get('GITHUB_REF') == 'refs/heads/main' and
            os.environ.get('GITHUB_REPOSITORY') == repository and repository in REPOSITORIES,
            'publication requires the repository main workflow')
    tag_valid(tag)
    with tempfile.TemporaryDirectory(prefix='verified-release-publication-') as temp:
        root = Path(temp)
        evidence = verified_attestation(run_id, expected, root)
        candidate = evidence['releases'][repository]
        require(candidate.get('tag') == tag, 'requested tag differs from acceptance')
        current = snapshot(repository, tag, root / 'current')
        require(current == candidate, 'draft bytes or release notes changed after acceptance')
        verify_images(current)
        partner_repo = 'soulteary/oc' if repository == 'soulteary/otterio' else 'soulteary/otterio'
        partner = evidence['releases'][partner_repo]
        partner_current = snapshot(partner_repo, partner['tag'], root / 'partner', draft=repository != 'soulteary/otterio')
        require(partner_current == partner, 'accepted partner changed or OC replacement is not published first')
        verify_images(partner_current)
        bytes_hashes = packaged_hashes(root / 'current', repository, tag)
        bytes_hashes.update(packaged_hashes(root / 'partner', partner_repo, partner['tag']))
        require(bytes_hashes == evidence['packagedPrograms'], 'accepted program hashes differ from actual draft archives')
        # Fail closed if the tag/body/assets changed while image checks ran.
        final = snapshot(repository, tag, root / 'rechecked')
        require(final == current, 'draft changed during publication verification')
        partner_final = snapshot(partner_repo, partner['tag'], root / 'partner-rechecked', draft=repository != 'soulteary/otterio')
        require(partner_final == partner_current, 'partner changed during publication verification')
        command(['gh', 'release', 'edit', tag, '--repo', repository, '--draft=false', '--latest=false'], write=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='operation', required=True)
    publication = sub.add_parser('publish')
    publication.add_argument('--repository', choices=REPOSITORIES, required=True)
    publication.add_argument('--tag', required=True)
    publication.add_argument('--acceptance-run-id', required=True)
    publication.add_argument('--attestation-sha256', required=True)
    args = parser.parse_args()
    try:
        publish(args.repository, args.tag, args.acceptance_run_id, args.attestation_sha256)
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError) as error:
        print('Publication refused: ' + str(error), file=__import__('sys').stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
