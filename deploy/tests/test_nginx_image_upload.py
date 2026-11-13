"""Safety checks for the narrowly scoped Nginx image-upload patch."""
import importlib.util
from pathlib import Path
import unittest

MODULE = Path(__file__).resolve().parents[1] / 'nginx' / 'patch_image_upload.py'
patcher = None
if MODULE.exists():
    spec = importlib.util.spec_from_file_location('patch_image_upload', MODULE)
    patcher = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(patcher)

PROXY_BODY = '''
        # A comment containing } must not end this location.
        add_header X-Example "literal { value }";
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        proxy_buffering off;
        proxy_cache off;
    '''
SOURCE = '''server {
    listen 80;
    location / { return 301 https://$host$request_uri; }
}
server {
    listen 443 ssl;
    client_max_body_size 100m;
    location / {''' + PROXY_BODY + '''}
}
'''


class ImageUploadPatch(unittest.TestCase):
    def patch(self, source):
        self.assertIsNotNone(patcher, 'Image-upload configuration patcher is not implemented')
        return patcher.patch_site(source)

    def test_targets_only_edit_routes_and_preserves_original_proxy(self):
        result = self.patch(SOURCE)
        self.assertIn('location = /v1/images/edits {', result)
        self.assertIn('location = /images/edits {', result)
        self.assertNotIn('location = /v1/images/generations', result)
        self.assertEqual(result.count('client_body_timeout 180s;'), 2)
        self.assertEqual(result.count(PROXY_BODY), 3)
        self.assertIn('location / { return 301 https://$host$request_uri; }', result)
        self.assertEqual(result.count('client_max_body_size 100m;'), 1)

    def test_reapplying_is_idempotent(self):
        first = self.patch(SOURCE)
        self.assertEqual(self.patch(first), first)

    def test_refuses_an_existing_unmanaged_edit_location(self):
        self.assertIsNotNone(patcher, 'Image-upload configuration patcher is not implemented')
        with self.assertRaises(ValueError):
            self.patch(SOURCE + '\nlocation = /v1/images/edits { return 403; }\n')

    def test_refuses_ambiguous_multiple_application_locations(self):
        self.assertIsNotNone(patcher, 'Image-upload configuration patcher is not implemented')
        with self.assertRaises(ValueError):
            self.patch(SOURCE + SOURCE)

    def test_refuses_missing_or_different_application_upstream(self):
        self.assertIsNotNone(patcher, 'Image-upload configuration patcher is not implemented')
        with self.assertRaises(ValueError):
            self.patch(SOURCE.replace('127.0.0.1:8080', '127.0.0.1:9000'))

    def test_refuses_unterminated_managed_block(self):
        self.assertIsNotNone(patcher, 'Image-upload configuration patcher is not implemented')
        with self.assertRaises(ValueError):
            self.patch('# BEGIN SUB2API_IMAGE_UPLOAD\n' + SOURCE)

    def test_refuses_dedicated_or_covering_locations_in_supported_syntaxes(self):
        locations = (
            'location = "/v1/images/edits" { return 403; }',
            "location = '/images/edits' { return 403; }",
            'location /v1/images/edits { return 403; }',
            'location ^~ /images/ { return 403; }',
            'location /v1/ { return 403; }',
            'location ~ ^/v1/images/edits$ { return 403; }',
            'location ~* "^/images/" { return 403; }',
            'location /unrelated/ { return 403; }',
        )
        for location in locations:
            with self.subTest(location=location), self.assertRaises(ValueError):
                self.patch(SOURCE.replace('    listen 443 ssl;',
                                          '    listen 443 ssl;\n    ' + location))

    def test_preserves_supported_acme_locations(self):
        for modifier in ('', '^~ '):
            with self.subTest(modifier=modifier):
                acme = ('    location ' + modifier + '/.well-known/acme-challenge/ {\n'
                        '        root /var/www/html;\n    }\n')
                source = SOURCE.replace('    listen 80;', '    listen 80;\n' + acme)
                result = self.patch(source)
                self.assertEqual(result.count(acme), 1)
                self.assertEqual(result.count(PROXY_BODY), 3)

    def test_discovers_quoted_root_and_proxy_arguments(self):
        source = SOURCE.replace('location / {', 'location "/" {').replace(
            'proxy_pass http://127.0.0.1:8080;',
            'proxy_pass "http://127.0.0.1:8080";')
        result = self.patch(source)
        self.assertEqual(result.count('proxy_pass "http://127.0.0.1:8080";'), 3)

    def test_does_not_discover_a_location_inside_multiline_quoted_text(self):
        fake = ('    add_header X-Example "literal\n'
                '    location / {\n'
                '        proxy_pass http://127.0.0.1:8080;\n'
                '    }\n    text";\n')
        source = SOURCE.replace('    listen 443 ssl;', '    listen 443 ssl;\n' + fake)
        source = source.replace(PROXY_BODY, PROXY_BODY.replace(':8080', ':9000'))
        with self.assertRaises(ValueError):
            self.patch(source)

    def test_does_not_discover_a_proxy_inside_multiline_quoted_text(self):
        fake = ('        add_header X-Example "literal\n'
                '        proxy_pass http://127.0.0.1:8080;\n'
                '        text";')
        source = SOURCE.replace('        proxy_pass http://127.0.0.1:8080;',
                                fake + '\n        proxy_pass http://127.0.0.1:9000;')
        with self.assertRaises(ValueError):
            self.patch(source)

    def test_ignores_fake_dedicated_locations_in_strings_and_comments(self):
        quoted = '        add_header X-Example "location = /v1/images/edits { deny all; }";\n'
        comment = '        # location = /images/edits { deny all; }\n'
        source = SOURCE.replace('        proxy_pass ', quoted + comment + '        proxy_pass ')
        result = self.patch(source)
        self.assertEqual(result.count(quoted), 3)
        self.assertEqual(result.count(comment), 3)

    def test_refuses_customized_managed_content(self):
        first = self.patch(SOURCE)
        modifications = (
            first.replace('client_body_timeout 180s;',
                          'client_body_timeout 180s;\n        deny all;', 1),
            first.replace('client_body_timeout 180s;', 'client_body_timeout 240s;', 1),
            first.replace('proxy_set_header Host $host;',
                          'proxy_set_header Host restricted.example;', 1),
        )
        for number, source in enumerate(modifications):
            with self.subTest(modification=number), self.assertRaises(ValueError):
                self.patch(source)

    def test_refuses_managed_blocks_moved_away_from_the_application_location(self):
        first = self.patch(SOURCE)
        start = first.index('    # BEGIN SUB2API_IMAGE_UPLOAD\n')
        end = first.index('    # END SUB2API_IMAGE_UPLOAD\n') + len('    # END SUB2API_IMAGE_UPLOAD\n')
        managed = first[start:end]
        source = first[:start] + first[end:]
        source = managed + source
        with self.assertRaises(ValueError):
            self.patch(source)

    def test_rejects_timeout_changes_outside_the_verified_policy(self):
        for timeout in (60, 179, 181, 600, True, 180.0):
            with self.subTest(timeout=timeout), self.assertRaises(ValueError):
                patcher.patch_site(SOURCE, timeout)


if __name__ == '__main__':
    unittest.main(verbosity=2)
