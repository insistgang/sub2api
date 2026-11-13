#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
export ROOT_DIR
python3 - <<'PY'
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile

source = (Path(os.environ['ROOT_DIR']) / 'deploy/Dockerfile.prebuilt').read_text()
command = source.split('\nRUN ', 1)[1].split('\n\n', 1)[0].replace('\\\n', '')
with tempfile.TemporaryDirectory(prefix='sub2api-prebuilt-test-') as directory:
    root = Path(directory)
    binary = root / 'sub2api'
    binary.write_text('#!/bin/sh\nprintf "%s\\n" "Sub2API test-version (commit: test-commit, built: test-date)"\n')
    command = command.replace('/app/sub2api', str(binary)).replace('/tmp/sub2api-version', str(root / 'version'))
    expected = dict(VERSION='test-version', COMMIT='test-commit', BUILD_DATE='test-date',
                    BINARY_SHA256=hashlib.sha256(binary.read_bytes()).hexdigest())
    cases = [('matching metadata', {}, True),
             ('wrong checksum', {'BINARY_SHA256': '0' * 64}, False),
             ('wrong version', {'VERSION': 'wrong-version'}, False),
             ('wrong commit', {'COMMIT': 'wrong-commit'}, False),
             ('wrong date', {'BUILD_DATE': 'wrong-date'}, False)]
    cases += [('missing ' + key, {key: ''}, False) for key in expected]
    for name, overrides, succeeds in cases:
        result = subprocess.run(['/bin/sh', '-c', command], env={**os.environ, **expected, **overrides},
                                capture_output=True, text=True)
        assert (result.returncode == 0) == succeeds, name + '\n' + result.stdout + result.stderr
print('prebuilt image checksum and metadata checks passed (9 cases)')
PY
