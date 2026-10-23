import importlib.util
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import urllib.error
import urllib.request
import urllib.response


SCRIPT = Path(__file__).resolve().parents[1] / 'workbuddy_smoke.py'
spec = importlib.util.spec_from_file_location('workbuddy_smoke', SCRIPT)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)

SECRET = 'fixture-secret-never-report'
MODEL = {'id': 'diagnostic-text', 'url': 'https://gateway.example/v1/chat/completions',
         'apiKey': SECRET, 'name': 'Private display name', 'reasoning': True}


class FakeReply(io.BytesIO):
    def __init__(self, body, status=200, content_type='application/json'):
        super().__init__(body)
        self.status = status
        self.headers = {'Content-Type': content_type}


class FakeOpener:
    def __init__(self, reply=None, error=None):
        self.reply = reply
        self.error = error
        self.requests = []

    def open(self, request, timeout):
        self.requests.append((request, timeout))
        if self.error:
            raise self.error
        return self.reply.pop(0) if isinstance(self.reply, list) else self.reply


def text_response(content='OK'):
    return json.dumps({'choices': [{'message': {'content': content},
                                  'finish_reason': 'stop'}]}).encode()


def tool_response(arguments='{}', name='diagnostic_ping'):
    return json.dumps({'choices': [{'message': {'content': None, 'tool_calls': [
        {'id': 'call_test', 'type': 'function',
         'function': {'name': name, 'arguments': arguments}}]},
        'finish_reason': 'tool_calls'}]}).encode()


def sse(*events, done=True):
    body = ': heartbeat\n\n' + ''.join('data: ' + json.dumps(event) + '\n\n'
                                        for event in events)
    return (body + ('data: [DONE]\n\n' if done else '')).encode()


class WorkBuddySmokeTest(unittest.TestCase):
    def run_checker(self, reply=None, flags=(), models=None, error=None, output=False):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / 'models.json'
            config.write_text(json.dumps(models if models is not None else [MODEL]))
            report_file = Path(temporary) / 'report.json'
            if output:
                report_file.write_text('old report')
                report_file.chmod(0o666)
            opener = FakeOpener(reply, error)
            args = ['--config', str(config), *flags]
            if output:
                args += ['--output', str(report_file)]
            stdout, stderr = io.StringIO(), io.StringIO()
            with patch.object(smoke.urllib.request, 'build_opener', return_value=opener) as build, \
                    patch('sys.stdout', stdout), patch('sys.stderr', stderr):
                code = smoke.main(args)
            saved = report_file.read_text() if output else None
            mode = stat.S_IMODE(report_file.stat().st_mode) if output else None
            return code, json.loads(stdout.getvalue()), opener, build.call_count, stderr.getvalue(), saved, mode

    def test_default_inventory_never_opens_network_or_claims_a_probe_passed(self):
        code, report, _, calls, _, _, _ = self.run_checker()
        self.assertEqual((code, calls), (0, 0))
        self.assertEqual(report['summary'], {'selected': 1, 'passed': 0, 'failed': 0, 'not_run': 1})
        self.assertIsNone(report['all_passed'])
        self.assertEqual(report['results'][0]['category'], 'not_run')
        self.assertIsNone(report['results'][0]['passed'])

    def test_valid_text_uses_configured_url_model_and_key(self):
        code, report, opener, _, _, _, _ = self.run_checker(FakeReply(text_response()), ['--live'])
        result = report['results'][0]
        self.assertEqual((code, result['passed'], result['text_chars']), (0, True, 2))
        request, timeout = opener.requests[0]
        self.assertEqual(request.full_url, MODEL['url'])
        self.assertEqual(request.get_header('Authorization'), 'Bearer ' + SECRET)
        self.assertEqual(json.loads(request.data)['model'], MODEL['id'])
        self.assertGreater(timeout, 0)
        self.assertNotIn(SECRET, json.dumps(report))

    def test_generic_chat_payload_uses_max_tokens_for_each_probe_shape(self):
        for stream in (False, True):
            for tool_call in (False, True):
                with self.subTest(stream=stream, tool_call=tool_call):
                    payload = smoke.payload_for(MODEL['id'], stream, tool_call)
                    self.assertEqual(payload.get('max_tokens'), 1024)
                    self.assertNotIn('max_completion_tokens', payload)

    def test_auto_and_named_tool_choice_payloads_are_distinct(self):
        for choice in ('auto', 'named'):
            with self.subTest(choice=choice):
                code, report, opener, _, _, _, _ = self.run_checker(
                    FakeReply(tool_response()), ['--live', '--tool-call', '--tool-choice', choice])
                self.assertEqual(code, 0)
                payload = json.loads(opener.requests[0][0].data)
                expected = 'auto' if choice == 'auto' else {
                    'type': 'function', 'function': {'name': 'diagnostic_ping'}}
                self.assertEqual(payload['tool_choice'], expected)
                self.assertEqual(payload['tools'][0]['function']['name'], 'diagnostic_ping')
                self.assertEqual(payload['max_tokens'], 1024)
                self.assertEqual(report['probe_settings']['tool_choice'], choice)

    def test_max_tokens_bounds_are_sent_as_max_tokens(self):
        for maximum in (128, 2048, 16384):
            with self.subTest(maximum=maximum):
                code, report, opener, _, _, _, _ = self.run_checker(
                    FakeReply(text_response()), ['--live', '--max-tokens', str(maximum)])
                self.assertEqual(code, 0)
                payload = json.loads(opener.requests[0][0].data)
                self.assertEqual(payload['max_tokens'], maximum)
                self.assertNotIn('max_completion_tokens', payload)
                self.assertEqual(report['probe_settings']['max_tokens'], maximum)

    def test_sanitized_reports_include_effective_probe_settings(self):
        code, report, _, calls, _, _, _ = self.run_checker()
        self.assertEqual((code, calls), (0, 0))
        self.assertEqual(report['probe_settings'], {
            'stream': False, 'tool_call': False, 'tool_choice': None,
            'max_tokens': 1024, 'timeout_seconds': 30.0})
        code, report, _, calls, _, saved, mode = self.run_checker(flags=[
            '--stream', '--tool-call', '--tool-choice', 'named', '--max-tokens', '2048', '--timeout', '12'],
            output=True)
        self.assertEqual((code, calls, mode), (0, 0, 0o600))
        self.assertEqual(report['probe_settings'], {
            'stream': True, 'tool_call': True, 'tool_choice': 'named',
            'max_tokens': 2048, 'timeout_seconds': 12.0})
        self.assertEqual(json.loads(saved), report)
        self.assertNotIn(SECRET, saved)

    def test_http_200_error_empty_success_and_http_503_fail(self):
        for reply, category in (
            (FakeReply(json.dumps({'error': {'message': SECRET}}).encode()), 'upstream_error'),
            (FakeReply(text_response('')), 'empty_response'),
            (FakeReply(b'{}'), 'invalid_response'),
            (FakeReply(text_response(), status=503), 'http_error'),
        ):
            with self.subTest(category=category):
                code, report, _, _, _, _, _ = self.run_checker(reply, ['--live'])
                self.assertEqual(code, 1)
                self.assertEqual(report['results'][0]['category'], category)
                self.assertFalse(report['all_passed'])

    def test_valid_sse_requires_content_finish_reason_and_done(self):
        events = ({'choices': [{'delta': {'content': 'OK'}, 'finish_reason': None}]},
                  {'choices': [{'delta': {}, 'finish_reason': 'stop'}]})
        code, report, opener, _, _, _, _ = self.run_checker(
            FakeReply(sse(*events), content_type='text/event-stream'), ['--live', '--stream'])
        self.assertEqual(code, 0)
        self.assertEqual(report['results'][0]['text_chars'], 2)
        self.assertTrue(json.loads(opener.requests[0][0].data)['stream'])
        for body, category in (
            (sse(*events, done=False), 'incomplete_stream'),
            (sse(events[0]), 'incomplete_stream'),
            (sse({'choices': [{'delta': {}, 'finish_reason': 'stop'}]}), 'empty_response'),
            (b'data: not-json\n\ndata: [DONE]\n\n', 'malformed_sse'),
            (sse({'error': {'message': SECRET}}), 'upstream_error'),
            (b'event: error\ndata: {}\n\n', 'upstream_error'),
        ):
            with self.subTest(category=category, body=body[:20]):
                code, report, _, _, _, _, _ = self.run_checker(
                    FakeReply(body, content_type='text/event-stream'), ['--live', '--stream'])
                self.assertEqual(code, 1)
                self.assertEqual(report['results'][0]['category'], category)

    def test_tool_probe_requires_requested_function_and_json_object_arguments(self):
        code, report, opener, _, _, _, _ = self.run_checker(
            FakeReply(tool_response()), ['--live', '--tool-call'])
        self.assertEqual(code, 0)
        self.assertEqual(report['results'][0]['tool_calls'], 1)
        payload = json.loads(opener.requests[0][0].data)
        self.assertEqual(payload['tool_choice'], 'auto')
        for body in (tool_response('{'), tool_response('[]'), tool_response('{}', 'other'), text_response()):
            code, report, _, _, _, _, _ = self.run_checker(FakeReply(body), ['--live', '--tool-call'])
            self.assertEqual(code, 1)
            self.assertEqual(report['results'][0]['category'], 'invalid_tool_call')

    def test_diagnostic_ping_rejects_unexpected_arguments(self):
        for choice in ('auto', 'named'):
            with self.subTest(choice=choice):
                code, report, _, _, _, _, _ = self.run_checker(
                    FakeReply(tool_response('{"unexpected": true}')),
                    ['--live', '--tool-call', '--tool-choice', choice])
                self.assertEqual(code, 1)
                self.assertEqual(report['results'][0]['category'], 'invalid_tool_call')

    def test_text_answers_never_pass_tool_probes_in_either_mode(self):
        for choice in ('auto', 'named'):
            for stream in (False, True):
                with self.subTest(choice=choice, stream=stream):
                    flags = ['--live', '--tool-call', '--tool-choice', choice]
                    reply = FakeReply(text_response())
                    if stream:
                        flags.append('--stream')
                        reply = FakeReply(sse(
                            {'choices': [{'delta': {'content': 'OK'}, 'finish_reason': None}]},
                            {'choices': [{'delta': {}, 'finish_reason': 'stop'}]}),
                            content_type='text/event-stream')
                    code, report, _, _, _, _, _ = self.run_checker(reply, flags)
                    self.assertEqual(code, 1)
                    self.assertEqual(report['results'][0]['category'], 'invalid_tool_call')

    def test_streamed_tool_arguments_are_assembled_before_validation(self):
        initial = {'choices': [{'delta': {'tool_calls': [
            {'index': 0, 'type': 'function', 'function': {'name': 'diagnostic_ping', 'arguments': '{'}}]},
            'finish_reason': None}]}
        ending = {'choices': [{'delta': {'tool_calls': [
            {'index': 0, 'function': {'arguments': '}'}}]}, 'finish_reason': 'tool_calls'}]}
        code, report, _, _, _, _, _ = self.run_checker(
            FakeReply(sse(initial, ending), content_type='text/event-stream'),
            ['--live', '--stream', '--tool-call'])
        self.assertEqual((code, report['results'][0]['tool_calls']), (0, 1))
        code, report, _, _, _, _, _ = self.run_checker(
            FakeReply(sse(initial, {'choices': [{'delta': {}, 'finish_reason': 'tool_calls'}]}),
                      content_type='text/event-stream'), ['--live', '--stream', '--tool-call'])
        self.assertEqual(code, 1)
        self.assertEqual(report['results'][0]['category'], 'invalid_tool_call')

    def test_filters_unknown_ids_and_mixed_results_are_truthful(self):
        other = {**MODEL, 'id': 'other-text'}
        code, report, _, _, _, _, _ = self.run_checker(
            flags=['--model', 'other-text', '--model', 'absent'], models=[MODEL, other])
        self.assertEqual(code, 1)
        self.assertEqual([r['model'] for r in report['results']], ['other-text', 'absent'])
        self.assertEqual(report['summary'], {'selected': 2, 'passed': 0, 'failed': 1, 'not_run': 1})

    def test_each_model_uses_its_own_credentials_and_any_failure_fails_aggregate(self):
        other = {**MODEL, 'id': 'second-text', 'apiKey': 'second-fixture-secret',
                 'url': 'https://second.example/custom/chat/completions'}
        replies = [FakeReply(text_response()), FakeReply(b'failure', status=503)]
        code, report, opener, _, _, _, _ = self.run_checker(replies, ['--live'], [MODEL, other])
        self.assertEqual(code, 1)
        self.assertEqual(report['summary'], {'selected': 2, 'passed': 1, 'failed': 1, 'not_run': 0})
        for configured, (request, _) in zip([MODEL, other], opener.requests):
            self.assertEqual(request.full_url, configured['url'])
            self.assertEqual(request.get_header('Authorization'), 'Bearer ' + configured['apiKey'])
            self.assertEqual(json.loads(request.data)['model'], configured['id'])
            self.assertNotIn(configured['apiKey'], json.dumps(report))

    def test_plain_http_remote_urls_credentials_queries_and_media_are_blocked(self):
        for change in ({'url': 'http://gateway.example/v1/chat/completions'},
                       {'url': 'https://user:password@gateway.example/v1/chat/completions'},
                       {'url': 'https://gateway.example/v1/chat/completions?key=' + SECRET},
                       {'url': 'https://gateway.example/v1/images/generations'},
                       {'id': 'gpt-image-1'}):
            with self.subTest(change=change):
                code, report, _, calls, _, _, _ = self.run_checker(
                    flags=['--live'], models=[{**MODEL, **change}])
                self.assertEqual((code, calls), (1, 0))
                self.assertFalse(report['results'][0]['passed'])
                self.assertNotIn(SECRET, json.dumps(report))
        for host in ('127.0.0.1:8080', '[::1]:8080', 'localhost:8080'):
            code, _, _, _, _, _, _ = self.run_checker(
                FakeReply(text_response()), ['--live'],
                [{**MODEL, 'url': 'http://' + host + '/v1/chat/completions'}])
            self.assertEqual(code, 0)

    def test_redirect_is_not_followed_with_authorization(self):
        class RedirectingHTTPS(urllib.request.HTTPSHandler):
            def __init__(self):
                super().__init__()
                self.seen = []

            def https_open(self, request):
                self.seen.append(request)
                reply = urllib.response.addinfourl(io.BytesIO(b''),
                    {'Location': 'https://other.example/stolen'}, request.full_url, 302)
                reply.msg = 'Found'
                return reply

        handler = RedirectingHTTPS()
        opener = urllib.request.build_opener(smoke.NoRedirect(), handler)
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / 'models.json'
            config.write_text(json.dumps([MODEL]))
            stdout = io.StringIO()
            with patch.object(smoke.urllib.request, 'build_opener', return_value=opener), patch('sys.stdout', stdout):
                code = smoke.main(['--config', str(config), '--live'])
            report = json.loads(stdout.getvalue())
        self.assertEqual((code, len(handler.seen)), (1, 1))
        self.assertEqual(report['results'][0]['category'], 'redirect_blocked')
        self.assertNotIn(SECRET, stdout.getvalue())

    def test_network_exceptions_and_reports_never_include_keys_or_bodies(self):
        code, report, _, _, stderr, saved, mode = self.run_checker(
            flags=['--live'], error=urllib.error.URLError('failure containing ' + SECRET), output=True)
        self.assertEqual((code, mode), (1, 0o600))
        self.assertEqual(json.loads(saved), report)
        self.assertNotIn(SECRET, saved + stderr)
        self.assertNotIn(MODEL['name'], saved)
        self.assertEqual(set(report['results'][0]),
                         {'model', 'host', 'path', 'http_status', 'latency_ms', 'passed',
                          'category', 'text_chars', 'tool_calls'})

    def test_secret_in_model_metadata_is_redacted_and_oversized_body_fails(self):
        code, report, _, _, _, _, _ = self.run_checker(models=[{**MODEL, 'id': 'prefix-' + SECRET}])
        self.assertEqual(code, 0)
        self.assertNotIn(SECRET, json.dumps(report))
        code, report, _, _, _, _, _ = self.run_checker(
            FakeReply(b'x' * (smoke.MAX_RESPONSE_BYTES + 1)), ['--live'])
        self.assertEqual(code, 1)
        self.assertEqual(report['results'][0]['category'], 'response_too_large')

    def test_output_cannot_overwrite_input_config(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / 'models.json'
            original = json.dumps([MODEL])
            config.write_text(original)
            stdout = io.StringIO()
            with patch('sys.stdout', stdout):
                code = smoke.main(['--config', str(config), '--output', str(config)])
            self.assertEqual(code, 1)
            self.assertEqual(config.read_text(), original)
            self.assertNotIn(SECRET, stdout.getvalue())

    def test_output_cannot_overwrite_config_through_a_hard_link(self):
        with tempfile.TemporaryDirectory() as temporary:
            config = Path(temporary) / 'models.json'
            original = json.dumps([MODEL])
            config.write_text(original)
            alias = Path(temporary) / 'report.json'
            alias.hardlink_to(config)
            with patch('sys.stdout', io.StringIO()):
                code = smoke.main(['--config', str(config), '--output', str(alias)])
            self.assertEqual(code, 1)
            self.assertEqual(config.read_text(), original)

    def test_timeout_is_bounded_and_invalid_configuration_is_sanitized(self):
        for value in ('0', '121', 'nan', 'inf'):
            with self.subTest(value=value), patch('sys.stderr', io.StringIO()):
                with self.assertRaises(SystemExit) as exit_info:
                    smoke.main(['--timeout', value])
                self.assertEqual(exit_info.exception.code, 2)
        code, report, _, calls, stderr, _, _ = self.run_checker(models={'apiKey': SECRET})
        self.assertEqual((code, calls), (1, 0))
        self.assertNotIn(SECRET, json.dumps(report) + stderr)

    def test_invalid_probe_arguments_fail_before_config_reads_without_echoing_values(self):
        invalid = [
            ['--max-tokens', value] for value in ('127', '16385', '1.5', 'nan', 'inf', SECRET)
        ] + [
            ['--tool-call', '--tool-choice', SECRET],
            ['--tool-choice', 'named'],
            ['--unknown-option', SECRET],
            ['--stream=' + SECRET],
        ]
        for flags in invalid:
            with self.subTest(flags=flags):
                stderr = io.StringIO()
                with patch.object(smoke.Path, 'read_text') as read_config, \
                        patch.object(smoke.urllib.request, 'build_opener') as build, \
                        patch('sys.stderr', stderr):
                    with self.assertRaises(SystemExit) as exit_info:
                        smoke.main(flags)
                self.assertEqual(exit_info.exception.code, 2)
                read_config.assert_not_called()
                build.assert_not_called()
                self.assertIn('error:', stderr.getvalue())
                self.assertNotIn(SECRET, stderr.getvalue())


if __name__ == '__main__':
    unittest.main()
