#!/usr/bin/env python3
"""Launch a packaged binary/image in a fresh setup environment; never connect to real data."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
from verify_frontend import PageAssets, local_asset

ROUTES = ('/', '/login', '/index.html', '/admin/upstream-balances', '/image-studio/')

def run(args, timeout=60):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Command failed: ' + args[0] + ' ' + result.stderr[-1000:])
    return result.stdout.strip()

def check_http(base):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    def get(path):
        with opener.open(base + path, timeout=10) as response:
            return response.status, response.headers.get('Content-Type', ''), response.read()
    for _ in range(90):
        try:
            code, _, _ = get('/setup/status')
            if code == 200:
                break
        except (OSError, urllib.error.URLError):
            time.sleep(0.5)
    else:
        raise RuntimeError('Release server did not start')
    pages = {}
    assets = set()
    for path in ROUTES:
        code, kind, raw = get(path)
        if code != 200 or 'text/html' not in kind or b'<html' not in raw.lower():
            raise RuntimeError('Frontend route failed: ' + path)
        parser = PageAssets()
        parser.feed(raw.decode('utf-8'))
        if not parser.scripts:
            raise RuntimeError('Frontend has no scripts: ' + path)
        for ref in parser.refs:
            asset = local_asset(path.lstrip('/') or 'index.html', ref)
            if asset:
                assets.add(asset)
        pages[path] = code
    for asset in sorted(assets):
        code, kind, raw = get(asset)
        if code != 200 or not raw or (asset.endswith(('.js', '.css')) and 'text/html' in kind):
            raise RuntimeError('Frontend asset failed: ' + asset)
    return {'pages': pages, 'assets_verified': len(assets), 'setup_status': 200, 'production_data_used': False, 'provider_model_requests_sent': 0}

def main(args):
    container = None
    process = None
    with tempfile.TemporaryDirectory(prefix='ikun-frontend-smoke-') as temp:
        folder = Path(temp)
        env = {**os.environ, 'AUTO_SETUP': 'false', 'SERVER_HOST': '127.0.0.1', 'DATA_DIR': str(folder/'data'), 'LOG_FILE_ENABLED': 'false', 'GIN_MODE': 'release'}
        try:
            if args.image:
                options = ['--platform', args.platform] if args.platform else []
                container = run(['docker', 'run', '-d', *options, '-p', '127.0.0.1::8080', '-e', 'AUTO_SETUP=false', '-e', 'LOG_FILE_ENABLED=false', args.image])
                meta = json.loads(run(['docker', 'inspect', container]))[0]
                port = meta['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort']
                version = run(['docker', 'exec', container, '/app/ikun2api', '--version'])
            else:
                binary = args.binary
                if args.archive:
                    with tarfile.open(args.archive) as archive:
                        member = next(m for m in archive if m.name.lstrip('./') == 'ikun2api' and m.isfile())
                        binary = folder/'ikun2api'
                        binary.write_bytes(archive.extractfile(member).read())
                        binary.chmod(0o755)
                if binary is None:
                    raise ValueError('Pass --binary, --archive or --image')
                binary = binary.resolve()
                version = run([str(binary), '--version'])
                with socket.socket() as sock:
                    sock.bind(('127.0.0.1', 0))
                    port = str(sock.getsockname()[1])
                env['SERVER_PORT'] = port
                process = subprocess.Popen([str(binary)], cwd=folder, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if 'IKUN2api ' + args.version + ' ' not in version:
                raise RuntimeError('Unexpected release version: ' + version)
            result = check_http('http://127.0.0.1:' + port)
            result['version'] = args.version
            result['target'] = args.platform or ('image' if args.image else 'binary')
            if args.output:
                args.output.parent.mkdir(parents=True, exist_ok=True)
                args.output.write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
            print(json.dumps(result))
        finally:
            if process is not None:
                process.terminate()
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            if container:
                # Only the disposable container created above is removed.
                subprocess.run(['docker', 'rm', '-f', container], capture_output=True, timeout=30)

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument('--binary', type=Path)
    source.add_argument('--archive', type=Path)
    source.add_argument('--image')
    parser.add_argument('--version', required=True)
    parser.add_argument('--platform')
    parser.add_argument('--output', type=Path)
    main(parser.parse_args())
