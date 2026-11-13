#!/usr/bin/env python3
"""Manual, unauthenticated paused-upload check; does not generate an image."""
import argparse
from datetime import datetime, timezone
import http.client
import json
import socket
import ssl
import time
import uuid


def probe(host, path, gap_seconds):
    result = {'host': host, 'path': path, 'pause_seconds': gap_seconds,
              'request_id': str(uuid.uuid4()), 'started_utc': datetime.now(timezone.utc).isoformat(),
              'http_status': None, 'body_bytes': 0, 'json_ok': False, 'passed': False}
    started = time.monotonic()
    print(json.dumps({'test': 'paused upload without a key or model call', **result}), flush=True)
    try:
        with socket.create_connection((host, 443), timeout=15) as tcp:
            with ssl.create_default_context().wrap_socket(tcp, server_hostname=host) as connection:
                connection.settimeout(20)
                headers = (f'POST {path} HTTP/1.1\r\nHost: {host}\r\n'
                           'User-Agent: Sub2API-Upload-Regression/20261113\r\n'
                           f'X-Request-ID: {result["request_id"]}\r\n'
                           'Content-Type: application/json\r\nContent-Length: 2\r\n'
                           'Connection: close\r\n\r\n')
                connection.sendall(headers.encode() + b'{')
                deadline = time.monotonic() + gap_seconds
                while time.monotonic() < deadline:
                    time.sleep(min(1, max(0, deadline - time.monotonic())))
                connection.sendall(b'}')
                with http.client.HTTPResponse(connection, method='POST') as response:
                    response.begin()
                    body = response.read(4096)
                    result['http_status'] = response.status
                    result['body_bytes'] = len(body)
                    try:
                        payload = json.loads(body)
                        result['json_ok'] = isinstance(payload, dict)
                        result['authentication_rejected'] = result['json_ok'] and payload.get('code') == 'API_KEY_REQUIRED'
                    except (ValueError, UnicodeDecodeError):
                        pass
                    result['passed'] = response.status == 401 and result['json_ok'] and result.get('authentication_rejected', False)
    except Exception as exc:
        result['transport_error'] = type(exc).__name__
    result['total_seconds'] = round(time.monotonic() - started, 2)
    print(json.dumps(result), flush=True)
    return result['passed']


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host', choices=['us.insistgang.top', 'api.insistgang.top'], default='us.insistgang.top')
    parser.add_argument('--path', choices=['/v1/images/edits', '/images/edits'], default='/v1/images/edits')
    parser.add_argument('--gap-seconds', type=int, default=65)
    args = parser.parse_args()
    if not 0 <= args.gap_seconds <= 120:
        parser.error('Use a pause between 0 and 120 seconds')
    raise SystemExit(0 if probe(args.host, args.path, args.gap_seconds) else 1)
