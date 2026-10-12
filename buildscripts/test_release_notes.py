#!/usr/bin/env python3
"""Regress the committed announcement and commit-bound release links."""

import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location('release_notes', Path(__file__).with_name('release-notes.py'))
NOTES = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(NOTES)
SOURCE = 'a' * 40
BASE = 'https://github.com/soulteary/otterio/blob/' + SOURCE + '/'


class LinkTests(unittest.TestCase):
    def test_each_document_resolves_from_its_own_directory(self):
        self.assertEqual(NOTES.rewrite_links('[guide](docs/releasing.md)', 'RELEASE_NOTES.md', SOURCE),
                         '[guide](' + BASE + 'docs/releasing.md)')
        self.assertEqual(NOTES.rewrite_links('[guide](../console-migration-preparation.md#configuration-preflight)',
                                            NOTES.SOURCES[1], SOURCE),
                         '[guide](' + BASE + 'docs/console-migration-preparation.md#configuration-preflight)')

    def test_external_protocol_relative_and_anchor_links_unchanged(self):
        text = '[web](https://example.test/a(b)) [mail](mailto:a@example.test) [cdn](//example.test/x) [part](#policy)'
        self.assertEqual(NOTES.rewrite_links(text, NOTES.SOURCES[1], SOURCE), text)

    def test_spaces_unicode_query_and_fragment_preserved(self):
        text = '[guide](<../a report 中文.md?raw=1#part> "Useful title")'
        self.assertEqual(NOTES.rewrite_links(text, NOTES.SOURCES[1], SOURCE),
                         '[guide](<' + BASE + 'docs/a%20report%20%E4%B8%AD%E6%96%87.md?raw=1#part> "Useful title")')

    def test_parentheses_escaped_destination_and_link_label_code(self):
        text = r'[`guide`](docs/a(b).md "A title") [escaped](docs/a\(b\).md)'
        expected = '[`guide`](' + BASE + 'docs/a%28b%29.md "A title") [escaped](' + BASE + 'docs/a%28b%29.md)'
        self.assertEqual(NOTES.rewrite_links(text, 'RELEASE_NOTES.md', SOURCE), expected)

    def test_images_and_root_relative_links_are_bound(self):
        text = '![diagram](/docs/diagram.svg)'
        self.assertEqual(NOTES.rewrite_links(text, NOTES.SOURCES[1], SOURCE),
                         '![diagram](' + BASE + 'docs/diagram.svg)')

    def test_reference_definition_retains_title_and_usage(self):
        text = '[guide][ref]\n[ref]: <../releasing.md#prepare> "Release guide"\n'
        self.assertEqual(NOTES.rewrite_links(text, NOTES.SOURCES[1], SOURCE),
                         '[guide][ref]\n[ref]: <' + BASE + 'docs/releasing.md#prepare> "Release guide"\n')

    def test_code_and_escaped_markdown_are_not_rewritten(self):
        text = ('`[inline](docs/one.md)`\n````sh\n[code](docs/two.md)\n```\n'
                '[still-code](docs/three.md)\n````\n~~~\n[tildes](docs/four.md)\n~~~\n'
                '    [indented](docs/five.md)\n\\[escaped](docs/six.md)\n')
        self.assertEqual(NOTES.rewrite_links(text, 'RELEASE_NOTES.md', SOURCE), text)

    def test_query_only_link_binds_current_document(self):
        self.assertEqual(NOTES.rewrite_links('[raw](?raw=1)', NOTES.SOURCES[1], SOURCE),
                         '[raw](' + BASE + NOTES.SOURCES[1] + '?raw=1)')

    def test_repository_escape_is_rejected(self):
        for target in ('../../../outside.md', '../%2e%2e/%2e%2e/outside.md'):
            with self.subTest(target=target), self.assertRaisesRegex(ValueError, 'escapes'):
                NOTES.rewrite_links('[bad](' + target + ')', NOTES.SOURCES[1], SOURCE)


class CommittedBodyTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='release-notes-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Release notes fixture')
        self.git('config', 'user.email', 'release-notes@example.test')
        (self.root / 'docs/releases').mkdir(parents=True)
        self.notes = '# Release notes\n\n[guide](docs/releasing.md)\n\n原有中文记录。\n'
        self.notice = ('# Full English announcement\n\n' + NOTES.PREPARATION_STATUS +
                       ' The selected policy is conditional on publication.\n\n'
                       'The old windows are incomplete.\n\n[restore](../restore.md#snapshot)\n')
        (self.root / NOTES.SOURCES[0]).write_text(self.notes)
        (self.root / NOTES.SOURCES[1]).write_text(self.notice)
        self.commit()

    def git(self, *arguments):
        return subprocess.check_output(['git', *arguments], cwd=self.root, stderr=subprocess.DEVNULL).decode().strip()

    def commit(self):
        self.git('add', '.')
        self.git('-c', 'commit.gpgsign=false', 'commit', '-qm', 'fixture')
        self.source = self.git('rev-parse', 'HEAD')

    def test_full_notice_and_original_notes_are_both_in_actual_body(self):
        body = NOTES.build_notes(self.source, self.root)
        base = 'https://github.com/soulteary/otterio/blob/' + self.source + '/'
        self.assertEqual(body, '# Release notes\n\n[guide](' + base + 'docs/releasing.md)\n\n原有中文记录。\n\n---\n\n'
                         '# Full English announcement\n\n' + NOTES.RELEASE_STATUS +
                         ' The selected policy is conditional on publication.\n\n'
                         'The old windows are incomplete.\n\n[restore](' + base + 'docs/restore.md#snapshot)\n')

    def test_release_status_is_conditional_and_committed_draft_status_is_retained(self):
        body = NOTES.build_notes(self.source, self.root)
        self.assertNotIn('publication pending', body)
        self.assertIn('Publication of this approved', body)
        self.assertIn('draft preparation alone does not.', body)
        self.assertEqual(NOTES.committed(self.root, 'show', self.source + ':' + NOTES.SOURCES[1]), self.notice)
        self.assertEqual((self.root / NOTES.SOURCES[1]).read_text(), self.notice)

    def test_unreviewed_announcement_status_is_refused(self):
        (self.root / NOTES.SOURCES[1]).write_text(self.notice.replace(NOTES.PREPARATION_STATUS, '**Already published.**'))
        self.commit()
        with self.assertRaisesRegex(ValueError, 'status must be reviewed explicitly'):
            NOTES.build_notes(self.source, self.root)

    def test_uncommitted_material_cannot_replace_tagged_notice(self):
        (self.root / NOTES.SOURCES[0]).write_text('uncommitted replacement notes')
        (self.root / NOTES.SOURCES[1]).write_text('uncommitted replacement notice')
        body = NOTES.build_notes(self.source, self.root)
        self.assertIn('Full English announcement', body)
        self.assertIn('原有中文记录。', body)
        self.assertNotIn('uncommitted replacement', body)

    def test_different_checkout_is_refused(self):
        previous = self.source
        (self.root / NOTES.SOURCES[0]).write_text('next commit')
        self.commit()
        with self.assertRaisesRegex(ValueError, 'selected source checkout'):
            NOTES.build_notes(previous, self.root)

    def test_missing_or_empty_notice_fails_closed(self):
        (self.root / NOTES.SOURCES[1]).unlink()
        self.commit()
        with self.assertRaisesRegex(ValueError, 'committed release material'):
            NOTES.build_notes(self.source, self.root)
        (self.root / NOTES.SOURCES[1]).write_text(' \n')
        self.commit()
        with self.assertRaisesRegex(ValueError, 'nonempty'):
            NOTES.build_notes(self.source, self.root)

    def test_invalid_source_rejected_before_git(self):
        for value in ('main', 'a' * 39, 'A' * 40, 'a' * 64, 'a' * 40 + ':RELEASE_NOTES.md'):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, 'full lowercase commit SHA'):
                NOTES.build_notes(value, Path('/does-not-exist'))

    def test_cli_writes_prepared_body_from_the_source_checkout(self):
        directory = self.root / 'buildscripts'
        directory.mkdir()
        script = directory / 'release-notes.py'
        shutil.copyfile(Path(NOTES.__file__), script)
        output = self.root / 'prepared-notes.md'
        result = subprocess.run([sys.executable, str(script), '--source-sha', self.source,
                                 '--output', str(output)], cwd=self.root, capture_output=True,
                                env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output.read_text(), NOTES.build_notes(self.source, self.root))

    def test_cli_failure_does_not_replace_existing_output(self):
        directory = self.root / 'buildscripts'
        directory.mkdir()
        script = directory / 'release-notes.py'
        shutil.copyfile(Path(NOTES.__file__), script)
        output = self.root / 'prepared-notes.md'
        output.write_bytes(b'keep the reviewed body')
        result = subprocess.run([sys.executable, str(script), '--source-sha', 'main',
                                 '--output', str(output)], cwd=self.root, capture_output=True,
                                env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(output.read_bytes(), b'keep the reviewed body')


if __name__ == '__main__':
    unittest.main()
