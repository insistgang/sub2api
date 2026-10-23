#!/usr/bin/env python3
"""Inventory WorkBuddy Chat models; probe them only with explicit --live."""

import argparse
import ipaddress
import json
import math
import os
from pathlib import Path
import re
import stat
import time
import urllib.error
import urllib.parse
import urllib.request


MAX_RESPONSE_BYTES = 1024 * 1024
FINISH_REASONS = {'stop', 'length', 'tool_calls'}


class ProbeFailure(Exception):
    """Only a fixed category is carried across the reporting boundary."""


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        return None


def bounded_timeout(value):
    try:
        seconds = float(value)
    except ValueError:
        raise argparse.ArgumentTypeError('timeout must be between 1 and 120 seconds') from None
    if not math.isfinite(seconds) or not 1 <= seconds <= 120:
        raise argparse.ArgumentTypeError('timeout must be between 1 and 120 seconds')
    return seconds


def strict_json(value):
    def reject_constant(_):
        raise ValueError('invalid JSON constant')

    return json.loads(value, parse_constant=reject_constant)


def result_for(model=None):
    return {'model': model, 'host': None, 'path': None, 'http_status': None,
            'latency_ms': None, 'passed': None, 'category': 'not_run',
            'text_chars': 0, 'tool_calls': 0}


def endpoint_for(model, secrets):
    url, key = model.get('url'), model.get('apiKey')
    if not isinstance(url, str) or not isinstance(key, str) or not key.strip() or '\r' in key or '\n' in key:
        raise ProbeFailure('invalid_config')
    try:
        parts = urllib.parse.urlsplit(url)
        host = parts.hostname
        port = parts.port
    except ValueError:
        raise ProbeFailure('invalid_endpoint') from None
    if (not host or parts.username is not None or parts.password is not None or
            parts.query or parts.fragment or any(ord(c) <= 32 for c in url)):
        raise ProbeFailure('invalid_endpoint')
    # A key embedded in a URL must never be requested, even when percent encoded.
    decoded_url = urllib.parse.unquote(url)
    if any(secret in decoded_url for secret in secrets):
        raise ProbeFailure('invalid_endpoint')
    try:
        loopback = ipaddress.ip_address(host).is_loopback
    except ValueError:
        loopback = host.lower() == 'localhost'
    if parts.scheme != 'https' and not (parts.scheme == 'http' and loopback):
        raise ProbeFailure('insecure_endpoint')
    if not parts.path.rstrip('/').endswith('/chat/completions'):
        raise ProbeFailure('unsupported_endpoint')
    if re.search(r'image|imagen|dall-e|video|sora|\bveo', model['id'], re.IGNORECASE):
        raise ProbeFailure('media_model_blocked')
    display_host = '[' + host + ']' if ':' in host else host
    return url, display_host + (':' + str(port) if port else ''), parts.path


def payload_for(model, stream, tool_call):
    payload = {'model': model, 'stream': stream, 'max_tokens': 256,
               'messages': [{'role': 'user', 'content': 'Reply with OK.'}]}
    if tool_call:
        payload['messages'][0]['content'] = 'Call diagnostic_ping with an empty JSON object.'
        payload['tools'] = [{'type': 'function', 'function': {
            'name': 'diagnostic_ping', 'description': 'A local diagnostic with no side effects.',
            'parameters': {'type': 'object', 'properties': {}, 'additionalProperties': False}}}]
        payload['tool_choice'] = {'type': 'function', 'function': {'name': 'diagnostic_ping'}}
    return payload


def text_length(content, category):
    if content is None:
        return 0
    if isinstance(content, str):
        return len(content.strip())
    if isinstance(content, list) and all(isinstance(part, dict) and
            part.get('type') == 'text' and isinstance(part.get('text'), str) for part in content):
        return sum(len(part['text'].strip()) for part in content)
    raise ProbeFailure(category)


def check_tools(calls):
    if not calls:
        raise ProbeFailure('invalid_tool_call')
    for call in calls:
        if not isinstance(call, dict) or call.get('type', 'function') != 'function':
            raise ProbeFailure('invalid_tool_call')
        function = call.get('function')
        if not isinstance(function, dict) or function.get('name') != 'diagnostic_ping':
            raise ProbeFailure('invalid_tool_call')
        arguments = function.get('arguments')
        try:
            parsed = strict_json(arguments) if isinstance(arguments, str) else None
        except (ValueError, TypeError):
            raise ProbeFailure('invalid_tool_call') from None
        if not isinstance(parsed, dict) or parsed:
            raise ProbeFailure('invalid_tool_call')


def reject_error(data):
    if isinstance(data, dict) and (data.get('error') is not None or
            data.get('type') == 'error' or data.get('object') == 'error'):
        raise ProbeFailure('upstream_error')


def validate_json(body, tool_call):
    try:
        data = strict_json(body)
    except (ValueError, UnicodeError):
        raise ProbeFailure('invalid_json') from None
    reject_error(data)
    choices = data.get('choices') if isinstance(data, dict) else None
    if not isinstance(choices, list) or len(choices) != 1 or not isinstance(choices[0], dict):
        raise ProbeFailure('invalid_response')
    choice = choices[0]
    message = choice.get('message')
    if not isinstance(message, dict) or choice.get('finish_reason') not in FINISH_REASONS:
        raise ProbeFailure('invalid_response')
    chars = text_length(message.get('content'), 'invalid_response')
    calls = message.get('tool_calls') or []
    if not isinstance(calls, list):
        raise ProbeFailure('invalid_tool_call')
    if tool_call:
        check_tools(calls)
        if choice['finish_reason'] != 'tool_calls':
            raise ProbeFailure('invalid_tool_call')
    elif not chars:
        raise ProbeFailure('empty_response')
    return chars, len(calls)


def validate_sse(body, tool_call):
    try:
        lines = body.decode('utf-8').splitlines()
    except UnicodeError:
        raise ProbeFailure('malformed_sse') from None
    # JSON errors are sometimes returned with HTTP 200 instead of an SSE body.
    if body.lstrip().startswith(b'{'):
        try:
            reject_error(strict_json(body))
        except (ValueError, UnicodeError):
            pass
        raise ProbeFailure('malformed_sse')
    chars, finished, done = 0, False, False
    calls = {}
    data_lines, event = [], ''
    finish_reason = None
    for line in lines:
        if line:
            if line.startswith(':'):
                continue
            field, separator, value = line.partition(':')
            if not separator or field not in {'data', 'event', 'id', 'retry'}:
                raise ProbeFailure('malformed_sse')
            value = value[1:] if value.startswith(' ') else value
            if field == 'data':
                data_lines.append(value)
            elif field == 'event':
                event = value
            continue
        if event == 'error':
            raise ProbeFailure('upstream_error')
        event = ''
        if not data_lines:
            continue
        payload = '\n'.join(data_lines)
        data_lines = []
        if payload == '[DONE]':
            done = True
            break
        try:
            chunk = strict_json(payload)
        except (ValueError, UnicodeError):
            raise ProbeFailure('malformed_sse') from None
        reject_error(chunk)
        choices = chunk.get('choices') if isinstance(chunk, dict) else None
        if choices == [] and isinstance(chunk.get('usage'), dict):
            continue
        if not isinstance(choices, list) or len(choices) != 1 or not isinstance(choices[0], dict):
            raise ProbeFailure('malformed_sse')
        choice = choices[0]
        delta = choice.get('delta')
        if not isinstance(delta, dict) or finished:
            raise ProbeFailure('malformed_sse')
        chars += text_length(delta.get('content'), 'malformed_sse')
        fragments = delta.get('tool_calls') or []
        if not isinstance(fragments, list):
            raise ProbeFailure('invalid_tool_call')
        for fragment in fragments:
            if not isinstance(fragment, dict) or type(fragment.get('index')) is not int or fragment['index'] < 0:
                raise ProbeFailure('invalid_tool_call')
            if fragment.get('type', 'function') != 'function':
                raise ProbeFailure('invalid_tool_call')
            function = fragment.get('function')
            if not isinstance(function, dict):
                raise ProbeFailure('invalid_tool_call')
            target = calls.setdefault(fragment['index'], {'type': 'function', 'function': {'name': '', 'arguments': ''}})
            for field in ('name', 'arguments'):
                value = function.get(field, '')
                if not isinstance(value, str):
                    raise ProbeFailure('invalid_tool_call')
                target['function'][field] += value
        finish_reason = choice.get('finish_reason')
        if finish_reason is not None:
            if finish_reason not in FINISH_REASONS:
                raise ProbeFailure('incomplete_stream')
            finished = True
    if not done or not finished:
        raise ProbeFailure('incomplete_stream')
    if tool_call:
        check_tools(list(calls.values()))
        if finish_reason != 'tool_calls':
            raise ProbeFailure('invalid_tool_call')
    elif not chars:
        raise ProbeFailure('empty_response')
    return chars, len(calls)


def read_body(reply, stream, started, timeout):
    if not stream:
        body = reply.read(MAX_RESPONSE_BYTES + 1)
    else:
        body, done_line = bytearray(), False
        while len(body) <= MAX_RESPONSE_BYTES:
            if time.monotonic() - started > timeout:
                raise ProbeFailure('timeout')
            line = reply.readline(MAX_RESPONSE_BYTES - len(body) + 1)
            if not line:
                break
            body.extend(line)
            if line.strip() in {b'data: [DONE]', b'data:[DONE]'}:
                done_line = True
            if done_line and not line.strip():
                break
        body = bytes(body)
    if len(body) > MAX_RESPONSE_BYTES:
        raise ProbeFailure('response_too_large')
    return body


def probe(model, secrets, live, timeout, stream, tool_call):
    result = result_for(model['id'])
    try:
        url, result['host'], result['path'] = endpoint_for(model, secrets)
    except ProbeFailure as error:
        result.update(passed=False, category=str(error))
        return result
    if not live:
        return result
    started = time.monotonic()
    try:
        request = urllib.request.Request(url, method='POST',
            data=json.dumps(payload_for(model['id'], stream, tool_call)).encode('utf-8'),
            headers={'Authorization': 'Bearer ' + model['apiKey'], 'Content-Type': 'application/json',
                     'Accept': 'text/event-stream' if stream else 'application/json'})
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=timeout) as reply:
            result['http_status'] = reply.status
            if not 200 <= reply.status < 300:
                raise ProbeFailure('redirect_blocked' if 300 <= reply.status < 400 else 'http_error')
            body = read_body(reply, stream, started, timeout)
            if stream and not reply.headers.get('Content-Type', '').lower().startswith('text/event-stream'):
                # Inspect only for a structured upstream error; never report its text.
                try:
                    reject_error(strict_json(body))
                except (ValueError, UnicodeError):
                    pass
                raise ProbeFailure('malformed_sse')
            chars, calls = validate_sse(body, tool_call) if stream else validate_json(body, tool_call)
            result.update(passed=True, category='passed', text_chars=chars, tool_calls=calls)
    except urllib.error.HTTPError as error:
        result['http_status'] = error.code
        result.update(passed=False, category='redirect_blocked' if 300 <= error.code < 400 else 'http_error')
        error.close()
    except ProbeFailure as error:
        result.update(passed=False, category=str(error))
    except TimeoutError:
        result.update(passed=False, category='timeout')
    except Exception:
        # Transport exceptions may contain credentials, URLs or response bodies.
        result.update(passed=False, category='network_error')
    result['latency_ms'] = round((time.monotonic() - started) * 1000, 1)
    return result


def redact(value, secrets):
    if isinstance(value, str):
        for secret in sorted(secrets, key=len, reverse=True):
            value = value.replace(secret, '[redacted]')
        return value
    if isinstance(value, list):
        return [redact(item, secrets) for item in value]
    if isinstance(value, dict):
        return {key: redact(item, secrets) for key, item in value.items()}
    return value


def report_for(results, live, category=None):
    summary = {'selected': len(results), 'passed': sum(r['passed'] is True for r in results),
               'failed': sum(r['passed'] is False for r in results),
               'not_run': sum(r['passed'] is None for r in results)}
    passed = False if category or summary['failed'] else (True if live and results else None)
    return {'mode': 'live' if live else 'dry-run', 'category': category or ('failed' if summary['failed'] else 'completed'),
            'all_passed': passed, 'summary': summary, 'results': results}


def save_report(path, text, config_path):
    flags = os.O_WRONLY | os.O_CREAT | getattr(os, 'O_NOFOLLOW', 0) | os.O_NONBLOCK
    descriptor = os.open(path, flags, 0o600)
    with os.fdopen(descriptor, 'w', encoding='utf-8') as output:
        target = os.fstat(output.fileno())
        if not stat.S_ISREG(target.st_mode):
            raise OSError('output must be a regular file')
        source = config_path.stat()
        if (target.st_dev, target.st_ino) == (source.st_dev, source.st_ino):
            raise OSError('output must not overwrite configuration')
        os.fchmod(output.fileno(), 0o600)
        output.truncate(0)
        output.write(text)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', type=Path, default=Path.home() / '.workbuddy/models.json')
    parser.add_argument('--live', action='store_true', help='send one short text or diagnostic function request per selected model')
    parser.add_argument('--model', action='append', default=[], help='exact model ID; repeat to select several')
    parser.add_argument('--timeout', type=bounded_timeout, default=30.0, help='socket timeout in seconds, 1–120 (default: 30)')
    parser.add_argument('--stream', action='store_true', help='require complete Chat Completions SSE')
    parser.add_argument('--tool-call', action='store_true', help='require a diagnostic_ping function call; no function is executed')
    parser.add_argument('--output', type=Path, help='explicitly save the sanitized JSON report with mode 600')
    args = parser.parse_args(argv)
    secrets = set()
    try:
        try:
            if args.output and (args.output.expanduser().resolve() == args.config.expanduser().resolve() or
                    (args.output.expanduser().exists() and args.config.expanduser().exists() and
                     args.output.expanduser().samefile(args.config.expanduser()))):
                raise ProbeFailure('output_error')
        except OSError:
            raise ProbeFailure('output_error') from None
        try:
            models = strict_json(args.config.expanduser().read_text(encoding='utf-8'))
        except Exception:
            raise ProbeFailure('invalid_config') from None
        if not isinstance(models, list) or not models:
            raise ProbeFailure('invalid_config')
        secrets = {model['apiKey'] for model in models if isinstance(model, dict) and
                   isinstance(model.get('apiKey'), str) and model['apiKey']}
        if any(not isinstance(model, dict) or not isinstance(model.get('id'), str) or
               not model['id'].strip() for model in models):
            raise ProbeFailure('invalid_config')
        by_id = {model['id']: model for model in models}
        if len(by_id) != len(models):
            raise ProbeFailure('invalid_config')
        selected = list(dict.fromkeys(args.model)) if args.model else list(by_id)
        results = []
        for model_id in selected:
            if model_id not in by_id:
                result = result_for(model_id)
                result.update(passed=False, category='model_not_found')
            else:
                result = probe(by_id[model_id], secrets, args.live, args.timeout, args.stream, args.tool_call)
            results.append(result)
        report = report_for(results, args.live)
    except ProbeFailure as error:
        report = report_for([], args.live, str(error))
    report = redact(report, secrets)
    text = json.dumps(report, ensure_ascii=True, indent=2) + '\n'
    if args.output and report['category'] != 'output_error':
        try:
            save_report(args.output.expanduser(), text, args.config.expanduser())
        except Exception:
            report.update(category='output_error', all_passed=False)
            text = json.dumps(report, ensure_ascii=True, indent=2) + '\n'
    print(text, end='')
    return 1 if report['all_passed'] is False else 0


if __name__ == '__main__':
    raise SystemExit(main())
