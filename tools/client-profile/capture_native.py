import http.server
import hashlib
import json
import os
import platform
import re
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path

root = Path(os.environ.get('IKUN_CAPTURE_OUTPUT_DIR', './captures'))
root.mkdir(mode=0o700, parents=True, exist_ok=True)
root.chmod(0o700)
dummy = 'ikun-local-capture-only'
clients = {
    'claude': os.environ.get('IKUN_CAPTURE_CLAUDE_BINARY', '/opt/ikun2api/clients/claude'),
    'codex': os.environ.get('IKUN_CAPTURE_CODEX_BINARY', '/opt/ikun2api/clients/codex'),
}
allowed = ['User-Agent', 'X-Stainless-Lang', 'X-Stainless-Package-Version', 'X-Stainless-OS', 'X-Stainless-Arch', 'X-Stainless-Runtime', 'X-Stainless-Runtime-Version', 'X-App', 'anthropic-version', 'originator', 'version', 'Accept', 'Accept-Encoding']

def command(kind, address):
    if kind == 'claude':
        flags = [] if os.environ.get('IKUN_CAPTURE_ENTRY_MODE', 'normal-print') == 'normal-print' else ['--bare']
        default_prompt = 'Hello \U0001f680 \u4f60\u597d\u4e16\u754c\u8fd9\u662f\u4e2d\u6587\u6d4b\u8bd5\u6d88\u606f\uff0c\u7528\u6765\u68c0\u67e5\u534f\u8bae\u3002' if os.environ.get('IKUN_CAPTURE_UNICODE') == 'true' else 'Reply OK.'
        prompt = os.environ.get('IKUN_CAPTURE_PROMPT', default_prompt)
        prefix = ['qemu-x86_64', '-cpu', 'Nehalem'] if os.environ.get('IKUN_CAPTURE_QEMU') == 'true' else []
        return prefix + [clients[kind]] + flags + ['--print', prompt, '--no-session-persistence', '--setting-sources', '', '--tools', '']
    values = {
        'model_provider': 'capture',
        'model': 'gpt-6.1-sol',
        'model_providers.capture.name': 'Local capture',
        'model_providers.capture.base_url': address + '/v1',
        'model_providers.capture.wire_api': 'responses',
        'model_providers.capture.env_key': 'IKUN_CAPTURE_API_KEY',
    }
    result = [clients[kind], 'exec', '--skip-git-repo-check', '--ephemeral', '--ignore-user-config', '--sandbox', 'read-only']
    for key, value in values.items():
        result += ['-c', key + '=' + json.dumps(value)]
    return result + ['Reply OK.']

def environment(home, address):
    return {
        'PATH': '/usr/local/bin:/usr/bin:/bin', 'HOME': str(home), 'TMPDIR': str(home),
        'LANG': 'en_SG.UTF-8', 'LC_ALL': 'en_SG.UTF-8', 'TZ': 'Asia/Singapore', 'TERM': 'dumb',
        'NO_PROXY': 'localhost,127.0.0.1', 'no_proxy': 'localhost,127.0.0.1',
        'ANTHROPIC_API_KEY': dummy, 'ANTHROPIC_BASE_URL': address,
        'CLAUDE_CONFIG_DIR': str(home), 'CODEX_HOME': str(home),
        'OPENAI_API_KEY': dummy, 'IKUN_CAPTURE_API_KEY': dummy,
        'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC': '1',
        'CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL': '1',
        'DISABLE_TELEMETRY': '1', 'DISABLE_ERROR_REPORTING': '1',
        'DISABLE_AUTOUPDATER': '1', 'DO_NOT_TRACK': '1',
        'BUN_JSC_useJIT': '0', 'BUN_JSC_useConcurrentJIT': '0',
    }

def read_exact(connection, size):
    data = bytearray()
    while len(data) < size:
        block = connection.recv(size - len(data))
        if not block:
            raise RuntimeError('Client closed before ClientHello was complete')
        data.extend(block)
    return bytes(data)

def capture(kind, tls):
    completed = threading.Event()
    observed = {}
    failures = []
    server = None
    if tls:
        listener = socket.socket()
        listener.bind(('127.0.0.1', 0))
        listener.listen(4)
        listener.settimeout(180)
        port = listener.getsockname()[1]
        def receive_tls():
            try:
                connection, _ = listener.accept()
                with connection:
                    connection.settimeout(10)
                    header = read_exact(connection, 5)
                    if header[0] != 22:
                        raise RuntimeError('Expected TLS handshake record')
                    length = int.from_bytes(header[3:5], 'big')
                    if length > 65535:
                        raise RuntimeError('TLS record exceeds capture limit')
                    record = header + read_exact(connection, length)
                    path = root / (kind + '-clienthello.bin')
                    path.write_bytes(record)
                    observed.update({'clienthello': str(path), 'bytes': len(record)})
            except Exception as error:
                failures.append(str(error))
            completed.set()
        threading.Thread(target=receive_tls, daemon=True).start()
    else:
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_GET(self):
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"data":[]}')
            def do_POST(self):
                credentials = self.headers.get('Authorization', '') or self.headers.get('x-api-key', '')
                if credentials not in (dummy, 'Bearer ' + dummy):
                    self.send_error(401)
                    return
                observed['headers'] = {key: self.headers[key] for key in allowed if self.headers.get(key)}
                observed['request_path'] = self.path
                observed['header_order'] = [key for key, value in self.headers.raw_items()]
                integrity = self.headers.get('x-claude-code-wiggly-dove', '')
                observed['integrity_header'] = {'present':bool(integrity),'bytes':len(integrity.encode()),'sha256':hashlib.sha256(integrity.encode()).hexdigest() if integrity else None}
                length = int(self.headers.get('Content-Length', '0'))
                if kind == 'claude' and 0 < length < 8 * 1024 * 1024:
                    body = json.loads(self.rfile.read(length))
                    for block in body.get('system', []):
                        match = re.search(r'cc_entrypoint=([a-zA-Z0-9_-]+)', block.get('text', ''))
                        if match:
                            observed['billing_entrypoint'] = match.group(1)
                        text = block.get('text', '')
                        if text.startswith('x-anthropic-billing-header'):
                            observed['cc_gateway_present'] = 'cc_gateway=' in text
                            observed['cch_present'] = 'cch=' in text
                            version = re.search(r'cc_version=([^;\s]+)', text)
                            parts = version.group(1).split('.',3) if version else []
                            observed['billing_version'] = '.'.join(parts[:3]) if parts else None
                            observed['billing_fingerprint'] = parts[3] if len(parts)>3 else None
                    billing = [block for block in body.get('system', []) if block.get('text', '').startswith('x-anthropic-billing-header')]
                    (root/'claude-body-for-comparison.json').write_text(json.dumps({'messages':body.get('messages',[]),'system':billing},ensure_ascii=True,indent=2)+'\n')
                self.send_response(401)
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"type":"error","error":{"type":"authentication_error","message":"Local capture complete"}}')
                completed.set()
        server = http.server.HTTPServer(('127.0.0.1', 0), Handler)
        port = server.server_port
        threading.Thread(target=server.serve_forever, daemon=True).start()
    address = ('https' if tls else 'http') + '://localhost:' + str(port)
    with tempfile.TemporaryDirectory(prefix='ikun-capture-') as directory:
        home = Path(directory)
        with tempfile.TemporaryFile() as log:
            process = subprocess.Popen(command(kind, address), cwd=directory, env=environment(home, address), stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
            deadline = time.monotonic() + (180 if kind == 'claude' else 45)
            while not completed.wait(0.1):
                if process.poll() is not None or time.monotonic() > deadline:
                    break
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
            if not observed:
                log.seek(0)
                raise RuntimeError(kind + ': ' + '; '.join(failures) + ': ' + log.read().decode('utf-8', 'replace')[-1800:])
    if server:
        server.shutdown()
        server.server_close()
    else:
        listener.close()
    observed.update({'client': kind, 'mode': 'tls' if tls else 'http', 'kernel': platform.release(), 'machine': platform.machine()})
    if kind == 'claude':
        observed['compatibility'] = 'qemu-x86_64 Nehalem; JIT disabled' if os.environ.get('IKUN_CAPTURE_QEMU') == 'true' else 'direct client execution'
        observed['capture_entry_mode'] = os.environ.get('IKUN_CAPTURE_ENTRY_MODE', 'normal-print')
    (root / (kind + ('-tls' if tls else '-headers') + '.json')).write_text(json.dumps(observed, indent=2) + '\n')
    print(json.dumps(observed), flush=True)

if __name__ == '__main__':
    for client in (sys.argv[1:] or clients):
        if client not in clients or not Path(clients[client]).is_file():
            raise RuntimeError('Set the explicit executable path for the selected client')
        capture(client, False)
        capture(client, True)
