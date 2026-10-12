#!/usr/bin/env python3
"""Prepare a release body from the exact tagged notes and migration notice."""

import argparse
from pathlib import Path
import posixpath
import re
import subprocess
import sys
from urllib.parse import quote, unquote, urlsplit, urlunsplit


ROOT = Path(__file__).resolve().parents[1]
SOURCES = ('RELEASE_NOTES.md', 'docs/releases/console-migration-announcement.en.md')
REPOSITORY = 'soulteary/otterio'
PREPARATION_STATUS = '**Status: accelerated migration approved; publication pending.**'
RELEASE_STATUS = ('**Release notice: accelerated migration selected.** Publication of this approved\n'
                  'Web-free release containing this notice makes the policy revision effective;\n'
                  'draft preparation alone does not.')


def source_valid(source):
    if not re.fullmatch(r'[0-9a-f]{40}', source):
        raise ValueError('source must be a full lowercase commit SHA')


def committed(root, *arguments):
    result = subprocess.run(['git', *arguments], cwd=root, capture_output=True, check=False)
    if result.returncode:
        raise ValueError('cannot read the selected committed release material')
    return result.stdout.decode('utf-8')


def bound_target(target, document, source):
    if target.startswith(('#', '//')) or re.match(r'^[A-Za-z][A-Za-z0-9+.-]*:', target):
        return target
    # Markdown backslash escapes belong to the destination, not the file name.
    target = re.sub(r'\\([!"#$%&\'()*+,\-./:;<=>?@\[\]\\^_`{|}~])', r'\1', target)
    parts = urlsplit(target)
    path = unquote(parts.path)
    if path.startswith('/'):
        resolved = posixpath.normpath(path.lstrip('/'))
    elif path:
        resolved = posixpath.normpath(posixpath.join(posixpath.dirname(document), path))
    else:
        resolved = document
    if resolved in ('.', '..') or resolved.startswith('../') or '\\' in resolved:
        raise ValueError('relative release link escapes the repository')
    url = f'https://github.com/{REPOSITORY}/blob/{source}/' + quote(resolved, safe='/-._~')
    return urlunsplit((*urlsplit(url)[:3], parts.query, parts.fragment))


def closing(text, start, opening, ending):
    depth = 1
    cursor = start + 1
    while cursor < len(text):
        if text[cursor] == '\\':
            cursor += 2
            continue
        if text[cursor] == opening:
            depth += 1
        elif text[cursor] == ending:
            depth -= 1
            if not depth:
                return cursor
        cursor += 1
    return None


def inline_links(line, document, source):
    result = []
    cursor = 0
    while cursor < len(line):
        if line[cursor] == '\\' and cursor + 1 < len(line):
            result.append(line[cursor:cursor + 2])
            cursor += 2
            continue
        if line[cursor] == '`':
            length = len(line[cursor:]) - len(line[cursor:].lstrip('`'))
            end = line.find('`' * length, cursor + length)
            while end != -1 and ((end and line[end - 1] == '`') or
                                 (end + length < len(line) and line[end + length] == '`')):
                end = line.find('`' * length, end + length)
            if end != -1:
                result.append(line[cursor:end + length])
                cursor = end + length
                continue
        if line[cursor] != '[':
            result.append(line[cursor])
            cursor += 1
            continue
        label_end = closing(line, cursor, '[', ']')
        if label_end is None or line[label_end + 1:label_end + 2] != '(':
            result.append(line[cursor])
            cursor += 1
            continue
        start = label_end + 2
        while start < len(line) and line[start] in ' \t':
            start += 1
        if line[start:start + 1] == '<':
            end = line.find('>', start + 1)
            if end == -1:
                result.append(line[cursor])
                cursor += 1
                continue
            start += 1
            after = end + 1
        else:
            end = start
            depth = 0
            while end < len(line):
                char = line[end]
                if char == '\\':
                    end += 2
                    continue
                if char == '(':
                    depth += 1
                elif char == ')':
                    if not depth:
                        break
                    depth -= 1
                elif char.isspace() and not depth:
                    break
                end += 1
            after = end
        # Verify an optional Markdown title without rewriting its content.
        while after < len(line) and line[after] in ' \t':
            after += 1
        if line[after:after + 1] in ('"', "'", '('):
            mark = line[after]
            if mark == '(':
                title_end = closing(line, after, '(', ')')
            else:
                title_end = after + 1
                while title_end < len(line):
                    if line[title_end] == '\\':
                        title_end += 2
                        continue
                    if line[title_end] == mark:
                        break
                    title_end += 1
                if title_end == len(line):
                    title_end = None
            if title_end is not None:
                after = title_end + 1
                while after < len(line) and line[after] in ' \t':
                    after += 1
        if line[after:after + 1] != ')':
            result.append(line[cursor])
            cursor += 1
            continue
        result.append(line[cursor:start] + bound_target(line[start:end], document, source) + line[end:after + 1])
        cursor = after + 1
    return ''.join(result)


def rewrite_links(text, document, source):
    result = []
    fence = None
    for line in text.splitlines(keepends=True):
        marker = re.match(r'^ {0,3}(`{3,}|~{3,})(.*)', line)
        if fence:
            result.append(line)
            if marker and marker[1][0] == fence[0] and len(marker[1]) >= fence[1] and not marker[2].strip():
                fence = None
            continue
        if marker:
            fence = (marker[1][0], len(marker[1]))
            result.append(line)
            continue
        reference = re.match(r'^( {0,3}\[[^\]\n]+\]:[ \t]*)(?:<([^>\n]*)>|(\S+))(.*)$', line)
        if reference:
            group = 2 if reference[2] is not None else 3
            start, end = reference.span(group)
            result.append(line[:start] + bound_target(line[start:end], document, source) + line[end:])
        elif line.startswith(('    ', '\t')):
            result.append(line)
        else:
            result.append(inline_links(line, document, source))
    return ''.join(result)


def build_notes(source, root=ROOT):
    source_valid(source)
    if committed(root, 'rev-parse', 'HEAD').strip() != source:
        raise ValueError('release material must be prepared from the selected source checkout')
    documents = []
    for path in SOURCES:
        text = committed(root, 'show', f'{source}:{path}')
        if not text.strip():
            raise ValueError('release notes and migration announcement must be nonempty')
        if path == SOURCES[1]:
            # Keep the committed preparation status as history, but render a
            # truthful conditional status in both the draft and public body.
            lead = re.match(r'^(# [^\n]+\n\n)' + re.escape(PREPARATION_STATUS), text)
            if not lead or text.count(PREPARATION_STATUS) != 1:
                raise ValueError('migration announcement preparation status must be reviewed explicitly')
            text = text[:lead.start() + len(lead[1])] + RELEASE_STATUS + text[lead.end():]
        documents.append(rewrite_links(text, path, source).rstrip())
    return '\n\n---\n\n'.join(documents) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        body = build_notes(args.source_sha)
        args.output.write_text(body, encoding='utf-8')
    except (ValueError, OSError) as error:
        print(f'Release body preparation failed: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
