"""Exercise draft metadata lookup and the actual OtterIO asset snapshot path."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit


SOURCE = Path(__file__).with_name('release-stage.py')
SPEC = importlib.util.spec_from_file_location('otterio_release_draft_snapshot', SOURCE)
stage = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(stage)

TAG = 'RELEASE.2026-10-13T10-02-51Z'
SOURCE_SHA = 'e' * 40
REPOSITORY = 'soulteary/otterio'
RELEASE_ID = 411540151


def metadata(repository=REPOSITORY, **changes):
    value = {
        'id': RELEASE_ID, 'tag_name': TAG, 'draft': True, 'prerelease': False,
        'target_commitish': SOURCE_SHA, 'body': 'Complete reviewed migration notice.\n',
        'assets': [], 'html_url': f'https://github.com/{repository}/releases/tag/untagged-fixture',
    }
    value.update(changes)
    return value


def old_releases(count=100):
    return [metadata(id=1000 + index,
                     tag_name=f'RELEASE.2026-09-{1 + index // 24:02d}T{index % 24:02d}-00-00Z',
                     draft=False) for index in range(count)]


class GitHubAPI:
    """A draft exists in listing/by-ID, while the published tag route is 404."""
    def __init__(self, pages, selected=None, repository=REPOSITORY, by_tag=None):
        self.pages = pages
        self.selected = selected or metadata(repository)
        self.repository = repository
        self.by_tag = by_tag
        self.calls = []
        self.requested_pages = []

    def __call__(self, path):
        self.calls.append(path)
        url = urlsplit(path)
        prefix = f'repos/{self.repository}'
        if url.path == prefix + '/releases':
            query = parse_qs(url.query)
            page = int(query.get('page', ['1'])[0])
            self.requested_pages.append(page)
            return copy.deepcopy(self.pages.get(page, []))
        if url.path == prefix + '/releases/tags/' + TAG:
            if self.by_tag is None:
                raise ValueError('published release tag endpoint returned HTTP 404')
            return copy.deepcopy(self.by_tag)
        if url.path == prefix + '/releases/' + str(RELEASE_ID):
            return copy.deepcopy(self.selected)
        if url.path == prefix + '/git/ref/tags/' + TAG:
            return {'object': {'type': 'commit', 'sha': SOURCE_SHA}}
        raise AssertionError('Unexpected GitHub request: ' + path)


def asset_fixture():
    programs = {name: ('fixture program ' + name).encode()
                for name in stage.asset_names(REPOSITORY, TAG)
                if name not in {'release-manifest.json', f'otterio-{TAG}-checksums.txt'}}
    programs['release-manifest.json'] = (json.dumps({
        'schema_version': 1, 'tag': TAG, 'source_sha': SOURCE_SHA,
        'images': [{'repository': 'ghcr.io/' + REPOSITORY,
                    'digest': 'sha256:' + 'a' * 64}],
    }, sort_keys=True) + '\n').encode()
    checksums = ''.join(hashlib.sha256(data).hexdigest() + '  ' + name + '\n'
                        for name, data in sorted(programs.items()))
    programs[f'otterio-{TAG}-checksums.txt'] = checksums.encode()
    release = metadata(assets=[{
        'id': 7000 + index, 'name': name, 'state': 'uploaded',
        'digest': 'sha256:' + hashlib.sha256(data).hexdigest(), 'size': len(data),
    } for index, (name, data) in enumerate(sorted(programs.items()))])
    return programs, release


class DraftMetadataTests(unittest.TestCase):
    def test_draft_on_second_page_is_read_by_id_despite_published_tag_404(self):
        listed = metadata(body='List metadata is not the final body.')
        current = metadata(body='Fresh by-ID complete notice.')
        github = GitHubAPI({1: old_releases(), 2: [listed]}, current)
        with patch.object(stage, 'api', side_effect=github):
            result = stage.release_metadata(REPOSITORY, TAG)
        self.assertEqual(result, current)
        self.assertIn(2, github.requested_pages)
        self.assertIn(f'repos/{REPOSITORY}/releases/{RELEASE_ID}', github.calls)

    def test_only_the_exact_tag_matches(self):
        github = GitHubAPI({1: [metadata(tag_name=TAG + '-backup'),
                               metadata(tag_name='RELEASE.2026-10-13T10-02-52Z')]})
        with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
            stage.release_metadata(REPOSITORY, TAG)

    def test_duplicate_exact_tags_on_different_pages_are_rejected(self):
        first = old_releases(99) + [metadata()]
        github = GitHubAPI({1: first, 2: [metadata(id=RELEASE_ID + 1)]})
        with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
            stage.release_metadata(REPOSITORY, TAG)
        self.assertIn(2, github.requested_pages)

    def test_duplicate_exact_tags_on_one_page_are_rejected(self):
        github = GitHubAPI({1: [metadata(), metadata(id=RELEASE_ID + 1)]})
        with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
            stage.release_metadata(REPOSITORY, TAG)

    def test_matched_release_id_must_be_a_positive_integer_before_id_request(self):
        for value in (None, 0, -1, True, str(RELEASE_ID), 1.5):
            with self.subTest(value=value):
                github = GitHubAPI({1: [metadata(id=value)]})
                with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
                    stage.release_metadata(REPOSITORY, TAG)
                self.assertTrue(all('/releases/' not in path for path in github.calls))

    def test_list_to_id_identity_or_stable_state_change_is_rejected(self):
        for changes in ({'id': RELEASE_ID + 1}, {'tag_name': 'RELEASE.2026-10-13T10-02-52Z'},
                        {'draft': False}, {'prerelease': True}):
            with self.subTest(changes=changes):
                github = GitHubAPI({1: [metadata()]}, metadata(**changes))
                with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
                    stage.release_metadata(REPOSITORY, TAG)

    def test_prerelease_list_record_cannot_be_treated_as_a_stable_draft(self):
        github = GitHubAPI({1: [metadata(prerelease=True)]})
        with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
            stage.release_metadata(REPOSITORY, TAG)

    def test_optional_state_accepts_a_stable_release_but_rejects_a_state_change(self):
        for draft in (True, False):
            with self.subTest(draft=draft):
                current = metadata(draft=draft)
                github = GitHubAPI({1: [current]}, current)
                with patch.object(stage, 'api', side_effect=github):
                    self.assertEqual(stage.release_metadata(REPOSITORY, TAG, draft=None), current)
                changed = GitHubAPI({1: [current]}, metadata(draft=not draft))
                with patch.object(stage, 'api', side_effect=changed), self.assertRaises(ValueError):
                    stage.release_metadata(REPOSITORY, TAG, draft=None)

    def test_required_published_partner_does_not_fall_back_to_a_draft(self):
        repository = 'soulteary/oc'
        github = GitHubAPI({1: [metadata(repository)]}, repository=repository)
        with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
            stage.release_metadata(repository, TAG, draft=False)
        self.assertEqual(github.requested_pages, [])

    def test_required_published_partner_rejects_draft_or_prerelease_metadata(self):
        repository = 'soulteary/oc'
        for record in (metadata(repository), metadata(repository, draft=False, prerelease=True)):
            with self.subTest(record=record):
                github = GitHubAPI({}, repository=repository, by_tag=record)
                with patch.object(stage, 'api', side_effect=github), self.assertRaises(ValueError):
                    stage.release_metadata(repository, TAG, draft=False)

    def test_required_published_partner_accepts_a_published_stable_release(self):
        repository = 'soulteary/oc'
        published = metadata(repository, draft=False)
        github = GitHubAPI({}, repository=repository, by_tag=published)
        with patch.object(stage, 'api', side_effect=github):
            self.assertEqual(stage.release_metadata(repository, TAG, draft=False), published)


class OtterIOSnapshotTests(unittest.TestCase):
    def test_actual_snapshot_downloads_and_hashes_all_eight_draft_assets(self):
        files, release = asset_fixture()
        github = GitHubAPI({1: old_releases(), 2: [release]}, release)
        downloads = []

        def download(arguments, binary=False, write=False):
            self.assertEqual(arguments[:4], ['gh', 'release', 'download', TAG])
            self.assertFalse(write)
            self.assertEqual(arguments[arguments.index('--repo') + 1], REPOSITORY)
            destination = Path(arguments[arguments.index('--dir') + 1])
            downloads.append(destination)
            for name, data in files.items():
                (destination / name).write_bytes(data)
            return ''

        with tempfile.TemporaryDirectory() as directory:
            destination = Path(directory) / 'assets'
            with patch.object(stage, 'api', side_effect=github), patch.object(stage, 'command', side_effect=download):
                result = stage.snapshot(REPOSITORY, TAG, destination)
            self.assertEqual(len(downloads), 1)
            self.assertEqual({file.name for file in destination.iterdir()}, set(files))
            self.assertEqual(result['assets'], {name: hashlib.sha256(data).hexdigest()
                                               for name, data in files.items()})
            self.assertEqual(result['sourceSHA'], SOURCE_SHA)
            self.assertEqual(result['releaseID'], RELEASE_ID)
            self.assertEqual(result['releaseNotesSHA256'], hashlib.sha256(release['body'].encode()).hexdigest())

    def test_changed_draft_state_fails_before_any_download_or_directory_creation(self):
        _, release = asset_fixture()
        github = GitHubAPI({1: [release]}, dict(release, draft=False))
        with tempfile.TemporaryDirectory() as directory:
            destination = Path(directory) / 'assets'
            with patch.object(stage, 'api', side_effect=github), patch.object(stage, 'command') as download:
                with self.assertRaises(ValueError):
                    stage.snapshot(REPOSITORY, TAG, destination)
            download.assert_not_called()
            self.assertFalse(destination.exists())


if __name__ == '__main__':
    unittest.main()
