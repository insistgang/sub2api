#!/usr/bin/env python3
"""Stage image-edit upload locations for simple root/ACME sites; never overwrite files."""
import argparse
from collections import namedtuple
import hashlib
import json
import os
from pathlib import Path

BEGIN = '# BEGIN SUB2API_IMAGE_UPLOAD'
END = '# END SUB2API_IMAGE_UPLOAD'
LOCATIONS = ('/v1/images/edits', '/images/edits')
UPSTREAM = 'http://127.0.0.1:8080'
ACME = '/.well-known/acme-challenge/'
Token = namedtuple('Token', 'kind value start end')


def nginx_tokens(text):
    """Keep offsets while excluding quoted/commented delimiters from discovery."""
    index = 0
    while index < len(text):
        char = text[index]
        if char.isspace():
            index += 1
            continue
        start = index
        if char == '#':
            end = text.find('\n', index)
            index = len(text) if end < 0 else end
            yield Token('comment', text[start:index], start, index)
            continue
        if char in ';{}':
            index += 1
            yield Token('symbol', char, start, index)
            continue
        value, quote = [], None
        while index < len(text):
            char = text[index]
            if char == '\\':
                if index + 1 == len(text):
                    raise ValueError('Unterminated Nginx escape; manual review required')
                following = text[index + 1]
                value.append({'t': '\t', 'r': '\r', 'n': '\n',
                              '\\': '\\', '"': '"', "'": "'"}.get(following, '\\' + following))
                index += 2
                continue
            if quote:
                if char == quote:
                    quote = None
                else:
                    value.append(char)
            elif char in ('"', "'"):
                quote = char
            elif char.isspace() or char in ';{}':
                break
            elif text.startswith('${', index):
                end = text.find('}', index + 2)
                if end < 0:
                    raise ValueError('Unterminated Nginx variable; manual review required')
                value.append(text[index:end + 1])
                index = end + 1
                continue
            else:
                value.append(char)
            index += 1
        if quote:
            raise ValueError('Unterminated Nginx quote; manual review required')
        yield Token('word', ''.join(value), start, index)


def without_managed_block(text):
    markers = []
    for token in nginx_tokens(text):
        if token.kind != 'comment' or 'SUB2API_IMAGE_UPLOAD' not in token.value:
            continue
        start = text.rfind('\n', 0, token.start) + 1
        if (token.value not in (BEGIN, END) or text[start:token.start].strip()
                or text[token.end:token.end + 1] != '\n'):
            raise ValueError('Managed block has an unexpected marker layout; manual review required')
        markers.append((token.value, start, token.end + 1))
    if not markers:
        return text, False
    if len(markers) != 2 or [marker[0] for marker in markers] != [BEGIN, END]:
        raise ValueError('Ambiguous or unterminated managed upload block; manual review required')
    return text[:markers[0][1]] + text[markers[1][2]:], True


def nginx_blocks(text):
    """Discover direct statements and block boundaries, without interpreting directives."""
    blocks, stack, words = [], [], []
    for token in nginx_tokens(text):
        if token.kind == 'comment':
            continue
        if token.kind == 'word':
            words.append(token)
        elif token.value == '{':
            if not words:
                raise ValueError('Unexpected Nginx block; manual review required')
            block = {'header': words, 'opening': token.start, 'closing': None,
                     'parent': stack[-1] if stack else None, 'directives': []}
            blocks.append(block)
            stack.append(block)
            words = []
        elif token.value == ';':
            if not words:
                raise ValueError('Unexpected Nginx directive; manual review required')
            if words[0].value == 'location':
                raise ValueError('Unsupported location layout; manual review required')
            if stack:
                stack[-1]['directives'].append([word.value for word in words])
            words = []
        elif token.value == '}':
            if words or not stack:
                raise ValueError('Unexpected Nginx closing brace; manual review required')
            stack.pop()['closing'] = token.start
    if words or stack:
        raise ValueError('Unterminated Nginx configuration; manual review required')
    return blocks


def patch_site(text, timeout_seconds=180):
    if type(timeout_seconds) is not int or timeout_seconds != 180:
        raise ValueError('Only the verified 180-second upload idle timeout is supported')
    original = text
    text, had_managed_block = without_managed_block(text)
    candidates = []
    for block in nginx_blocks(text):
        header = [word.value for word in block['header']]
        if header[0] != 'location':
            continue
        parent = block['parent']
        if parent is None or [word.value for word in parent['header']] != ['server']:
            raise ValueError('Locations must be directly inside server blocks; manual review required')
        if header in (['location', ACME], ['location', '^~', ACME]):
            continue
        if header != ['location', '/']:
            raise ValueError('Only generic root and ACME locations are supported; manual review required')
        proxies = [directive for directive in block['directives'] if directive[0] == 'proxy_pass']
        if proxies == [['proxy_pass', UPSTREAM]]:
            candidates.append(block)
        elif proxies:
            raise ValueError('Unexpected application upstream; manual review required')
    if len(candidates) != 1:
        raise ValueError('Expected exactly one application location proxying 127.0.0.1:8080')
    application = candidates[0]
    location_start = application['header'][0].start
    start = text.rfind('\n', 0, location_start) + 1
    indent = text[start:location_start]
    if indent.strip():
        raise ValueError('Application location must start on its own line; manual review required')
    body = text[application['opening'] + 1:application['closing']]
    if any(directive[0] == 'client_body_timeout' for directive in application['directives']):
        raise ValueError('Existing application upload timeout requires manual review')
    blocks = [indent + BEGIN + '\n']
    for path in LOCATIONS:
        blocks.append(f'{indent}location = {path} {{\n{indent}    client_body_timeout {timeout_seconds}s;' + body + '}\n')
    blocks.append(indent + END + '\n')
    patched = text[:start] + ''.join(blocks) + text[start:]
    if had_managed_block and patched != original:
        raise ValueError('Managed upload content or placement changed; manual review required')
    return patched


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--timeout-seconds', type=int, default=180,
                        help='upload idle timeout; only the verified value 180 is supported')
    args = parser.parse_args()
    original = args.source.read_text(encoding='utf-8')
    try:
        patched = patch_site(original, args.timeout_seconds)
    except ValueError as error:
        parser.error(str(error))
    descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w', encoding='utf-8') as handle:
        handle.write(patched)
    print(json.dumps({'output': str(args.output), 'timeout_seconds': args.timeout_seconds,
                      'source_sha256': hashlib.sha256(original.encode()).hexdigest(),
                      'output_sha256': hashlib.sha256(patched.encode()).hexdigest()}))


if __name__ == '__main__':
    main()
