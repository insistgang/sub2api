import base64
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import workbuddy_gemini_image_mcp as server


PNG = base64.b64decode(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9ZQmcAAAAASUVORK5CYII='
)


def response(parts=None):
    if parts is None:
        parts = [{'inlineData': {'mimeType': 'image/png',
                                 'data': base64.b64encode(PNG).decode()}}]
    return {'modelVersion': server.MODELS[0],
            'candidates': [{'finishReason': 'STOP', 'content': {'parts': parts}}]}


class GeminiImageTests(unittest.TestCase):
    def test_native_request_contains_requested_resolution_and_modality(self):
        model, body = server.make_payload({'prompt': 'A blue teapot',
                                          'model': server.MODELS[2],
                                          'aspect_ratio': '16:9', 'image_size': '2K'})
        self.assertEqual(model, server.MODELS[2])
        self.assertEqual(body['contents'][0]['parts'], [{'text': 'A blue teapot'}])
        self.assertEqual(body['generationConfig'], {
            'responseModalities': ['TEXT', 'IMAGE'],
            'imageConfig': {'aspectRatio': '16:9', 'imageSize': '2K'}})

    def test_invalid_arguments_are_rejected_before_network(self):
        for args in ({'prompt': ''}, {'prompt': 'ok', 'model': '../bad'},
                     {'prompt': 'ok', 'image_size': '1k'},
                     {'prompt': 'ok', 'aspect_ratio': 'nonsense'},
                     {'prompt': 'ok', 'output_dir': '/unexpected'}):
            with self.subTest(args=args), self.assertRaises(ValueError):
                server.make_payload(args)

    def test_final_images_are_extracted_without_thought_images(self):
        raw = response()['candidates'][0]['content']['parts'][0]
        parts = [{'text': 'Here is your picture'}, {**raw, 'thought': True}, raw]
        self.assertEqual(server.decode_images(response(parts)), [('image/png', PNG)])

    def test_missing_or_malformed_image_is_not_a_success(self):
        bad = [response([{'text': 'No image'}]),
               response([{'inlineData': {'mimeType': 'image/png', 'data': '%%%'}}]),
               response([{'inlineData': {'mimeType': 'image/png',
                                        'data': base64.b64encode(b'not an image').decode()}}])]
        for data in bad:
            with self.subTest(data=data), self.assertRaises(ValueError):
                server.decode_images(data)

    def test_save_preserves_bytes_and_mcp_returns_viewable_image(self):
        with tempfile.TemporaryDirectory() as folder:
            env = {'GEMINI_IMAGE_OUTPUT_DIR': folder}
            with patch.dict(os.environ, env), patch.object(server, 'request_image', return_value=response()):
                result = server.call_tool({'name': 'generate_gemini_image',
                                           'arguments': {'prompt': 'A blue teapot'}})
            self.assertFalse(result.get('isError', False))
            info = json.loads(result['content'][0]['text'])
            path = Path(info['images'][0]['path'])
            self.assertEqual(path.parent, Path(folder).resolve())
            self.assertEqual(path.read_bytes(), PNG)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(base64.b64decode(result['content'][1]['data']), PNG)
            self.assertEqual(result['content'][1]['mimeType'], 'image/png')

    def test_connection_never_sends_existing_key_to_another_origin(self):
        with tempfile.TemporaryDirectory() as folder:
            p = Path(folder) / 'models.json'
            p.write_text(json.dumps([{'id': 'source-gemini', 'apiKey': 'test-only-key',
                                     'url': 'https://gateway.example/v1/chat/completions'}]))
            env = {'WORKBUDDY_MODELS_CONFIG': str(p),
                   'GEMINI_IMAGE_AUTH_MODEL': 'source-gemini',
                   'GEMINI_IMAGE_BASE_URL': 'https://other.example/antigravity/v1beta'}
            with patch.dict(os.environ, env), self.assertRaises(ValueError):
                server.load_connection()

    def test_redirect_is_not_followed_with_credentials(self):
        req = server.urllib.request.Request('https://gateway.example/image')
        self.assertIsNone(server.NoRedirect().redirect_request(
            req, io.BytesIO(), 302, 'Moved', {}, 'https://other.example/image'))

    def test_stdio_initialization_and_model_discovery(self):
        messages = [
            {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize',
             'params': {'protocolVersion': '2024-11-05', 'capabilities': {},
                        'clientInfo': {'name': 'test', 'version': '1'}}},
            {'jsonrpc': '2.0', 'method': 'notifications/initialized'},
            {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'},
            {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call',
             'params': {'name': 'unknown', 'arguments': {}}}]
        proc = subprocess.run([sys.executable, str(Path(server.__file__))],
                              input=''.join(json.dumps(m) + '\n' for m in messages),
                              text=True, capture_output=True, check=True, timeout=5)
        replies = [json.loads(line) for line in proc.stdout.splitlines()]
        self.assertEqual([r['id'] for r in replies], [1, 2, 3])
        self.assertEqual(replies[0]['result']['protocolVersion'], '2024-11-05')
        tool = replies[1]['result']['tools'][0]
        self.assertEqual(tool['inputSchema']['properties']['model']['enum'], list(server.MODELS))
        self.assertTrue(replies[2]['result']['isError'])


if __name__ == '__main__':
    unittest.main()
