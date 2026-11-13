#!/usr/bin/env python3
"""WorkBuddy stdio MCP for native Gemini images through an existing Sub2API key."""

import base64
import binascii
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


MODELS = ('gemini-3.1-flash-image', 'gemini-3.1-flash-image-preview',
          'gemini-3-pro-image', 'gemini-3-pro-image-preview')
ASPECT_RATIOS = ('1:1', '2:3', '3:2', '3:4', '4:3', '4:5', '5:4', '9:16', '16:9', '21:9')
IMAGE_SIZES = ('1K', '2K', '4K')
EXTENSIONS = {'image/png': '.png', 'image/jpeg': '.jpg', 'image/webp': '.webp'}
MAX_RESPONSE_BYTES = 64 * 1024 * 1024
TOOL = {
    'name': 'generate_gemini_image',
    'description': 'Generate an image with Gemini through Sub2API, save it locally, and return a preview. '
                   'Defaults to Gemini 3.1 Flash Image, 1K square. Gateway aliases may share an upstream model; '
                   'the result reports upstream_model.',
    'inputSchema': {
        'type': 'object', 'required': ['prompt'], 'additionalProperties': False,
        'properties': {
            'prompt': {'type': 'string', 'minLength': 1, 'maxLength': 32000},
            'model': {'type': 'string', 'enum': list(MODELS), 'default': MODELS[0],
                      'description': 'Requested gateway model ID; gateway mappings determine the actual upstream model.'},
            'aspect_ratio': {'type': 'string', 'enum': list(ASPECT_RATIOS), 'default': '1:1'},
            'image_size': {'type': 'string', 'enum': list(IMAGE_SIZES), 'default': '1K'},
        },
    },
    'annotations': {'readOnlyHint': False, 'destructiveHint': False,
                    'idempotentHint': False, 'openWorldHint': True},
}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def make_payload(args):
    if not isinstance(args, dict) or set(args) - set(TOOL['inputSchema']['properties']):
        raise ValueError('Unsupported tool arguments')
    prompt = args.get('prompt')
    if not isinstance(prompt, str) or not prompt.strip() or len(prompt) > 32000:
        raise ValueError('prompt must contain 1–32000 characters')
    model = args.get('model', MODELS[0])
    ratio = args.get('aspect_ratio', '1:1')
    size = args.get('image_size', '1K')
    if model not in MODELS or ratio not in ASPECT_RATIOS or size not in IMAGE_SIZES:
        raise ValueError('Unsupported model, aspect_ratio, or image_size')
    return model, {
        'contents': [{'role': 'user', 'parts': [{'text': prompt}]}],
        'generationConfig': {'responseModalities': ['TEXT', 'IMAGE'],
                             'imageConfig': {'aspectRatio': ratio, 'imageSize': size}},
    }


def load_connection():
    path = Path(os.environ.get('WORKBUDDY_MODELS_CONFIG',
                               str(Path.home() / '.workbuddy/models.json'))).expanduser()
    source_id = os.environ.get('GEMINI_IMAGE_AUTH_MODEL', '')
    root = os.environ.get('GEMINI_IMAGE_BASE_URL', '').rstrip('/')
    models = json.loads(path.read_text())
    matches = [m for m in models if isinstance(m, dict) and m.get('id') == source_id]
    if not source_id or len(matches) != 1 or not root:
        raise ValueError('Configure GEMINI_IMAGE_AUTH_MODEL and GEMINI_IMAGE_BASE_URL')
    source = matches[0]
    key = source.get('apiKey')
    target = urllib.parse.urlsplit(root)
    origin = urllib.parse.urlsplit(source.get('url', ''))
    if (not isinstance(key, str) or not key.strip() or '\n' in key or '\r' in key or
            target.scheme != 'https' or target.username or target.password or
            target.query or target.fragment or not target.path.endswith('/v1beta') or
            (target.scheme, target.hostname, target.port) !=
            (origin.scheme, origin.hostname, origin.port)):
        raise ValueError('Gemini endpoint must use HTTPS on the existing credential origin')
    return root, key


def request_image(model, payload):
    root, key = load_connection()
    req = urllib.request.Request(
        root + '/models/' + model + ':generateContent',
        data=json.dumps(payload).encode(),
        headers={'Authorization': 'Bearer ' + key, 'Content-Type': 'application/json'},
        method='POST')
    try:
        with urllib.request.build_opener(NoRedirect()).open(req, timeout=240) as response:
            body = response.read(MAX_RESPONSE_BYTES + 1)
        if len(body) > MAX_RESPONSE_BYTES:
            raise ValueError('Image response exceeds 64 MiB')
        return json.loads(body)
    except urllib.error.HTTPError as exc:
        # Avoid echoing upstream bodies: they can contain credentials or prompt text.
        exc.close()
        raise ValueError(f'Image endpoint returned HTTP {exc.code}; inspect the gateway for details') from None
    except (urllib.error.URLError, TimeoutError):
        raise ValueError('Image endpoint connection failed or timed out; no automatic retry was made') from None


def decode_images(response):
    images = []
    for candidate in response.get('candidates', []):
        if candidate.get('finishReason') not in (None, 'STOP'):
            continue
        for part in candidate.get('content', {}).get('parts', []):
            if part.get('thought'):
                continue
            inline = part.get('inlineData') or part.get('inline_data')
            if not inline:
                continue
            mime = inline.get('mimeType', inline.get('mime_type'))
            if mime not in EXTENSIONS:
                raise ValueError('Unsupported image MIME type from upstream')
            try:
                data = base64.b64decode(inline.get('data', ''), validate=True)
            except (ValueError, TypeError, binascii.Error):
                raise ValueError('Invalid base64 image from upstream') from None
            valid = ((mime == 'image/png' and data.startswith(b'\x89PNG\r\n\x1a\n') and
                      data.endswith(b'IEND\xaeB`\x82')) or
                     (mime == 'image/jpeg' and data.startswith(b'\xff\xd8') and data.endswith(b'\xff\xd9')) or
                     (mime == 'image/webp' and data.startswith(b'RIFF') and data[8:12] == b'WEBP'))
            if not valid:
                raise ValueError('Image bytes are missing, truncated, or do not match their MIME type')
            images.append((mime, data))
    if not images:
        raise ValueError('Upstream returned no completed final image')
    return images


def generate_image(args):
    model, payload = make_payload(args)
    started = time.monotonic()
    response = request_image(model, payload)
    images = decode_images(response)
    folder = Path(os.environ.get('GEMINI_IMAGE_OUTPUT_DIR',
                                  str(Path.home() / 'Pictures/Gemini Images'))).expanduser().resolve()
    folder.mkdir(parents=True, exist_ok=True, mode=0o700)
    result = {'model': model, 'upstream_model': response.get('modelVersion'),
              'aspect_ratio': args.get('aspect_ratio', '1:1'),
              'requested_image_size': args.get('image_size', '1K'), 'images': []}
    for mime, data in images:
        name = model + '-' + datetime.now().strftime('%Y%m%d-%H%M%S') + '-' + uuid.uuid4().hex[:8]
        path = folder / (name + EXTENSIONS[mime])
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as image_file:
            image_file.write(data)
        result['images'].append({'path': str(path), 'mime_type': mime, 'bytes': len(data),
                                 'sha256': hashlib.sha256(data).hexdigest()})
    result['elapsed_seconds'] = round(time.monotonic() - started, 2)
    return result, images


def call_tool(params):
    try:
        if not isinstance(params, dict) or params.get('name') != TOOL['name']:
            raise ValueError('Unknown tool')
        result, images = generate_image(params.get('arguments', {}))
        content = [{'type': 'text', 'text': json.dumps(result, ensure_ascii=False)}]
        content += [{'type': 'image', 'mimeType': mime, 'data': base64.b64encode(data).decode()}
                    for mime, data in images]
        return {'content': content, 'isError': False}
    except ValueError as exc:
        return {'content': [{'type': 'text', 'text': str(exc)}], 'isError': True}
    except Exception:
        return {'content': [{'type': 'text', 'text': 'Image tool failed; check local configuration and output permissions'}],
                'isError': True}


def handle(message):
    if 'id' not in message:
        return None
    method = message.get('method')
    if method == 'initialize':
        requested = message.get('params', {}).get('protocolVersion')
        version = requested if requested in ('2024-11-05', '2025-03-26', '2025-06-18') else '2024-11-05'
        result = {'protocolVersion': version, 'capabilities': {'tools': {}},
                  'serverInfo': {'name': 'sub2api-gemini-images', 'version': '1.0.0'}}
    elif method == 'ping':
        result = {}
    elif method == 'tools/list':
        result = {'tools': [TOOL]}
    elif method == 'tools/call':
        result = call_tool(message.get('params', {}))
    else:
        return {'jsonrpc': '2.0', 'id': message['id'],
                'error': {'code': -32601, 'message': 'Method not found'}}
    return {'jsonrpc': '2.0', 'id': message['id'], 'result': result}


def main():
    for line in sys.stdin:
        try:
            message = json.loads(line)
            if not isinstance(message, dict):
                raise ValueError('Expected JSON-RPC object')
            reply = handle(message)
        except (ValueError, TypeError, AttributeError):
            reply = {'jsonrpc': '2.0', 'id': None,
                     'error': {'code': -32700, 'message': 'Invalid JSON-RPC message'}}
        if reply is not None:
            print(json.dumps(reply, ensure_ascii=False), flush=True)


if __name__ == '__main__':
    main()
