import argparse
import fcntl
import json
import os
import re
import shutil
import signal
import subprocess
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path


def remove_session(path, runtime):
    if path.parent != runtime or not path.name.startswith('session-') or path.is_symlink():
        raise RuntimeError('Refusing to remove an unexpected session path')
    if path.exists():
        shutil.rmtree(path)


def terminate(process):
    if process is None:
        return
    if process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.wait(timeout=3)


def prepare_session(runtime, preferences, downloads, language_pack):
    session = Path(tempfile.mkdtemp(prefix='session-', dir=runtime))
    try:
        directories = {name: session / name for name in ['home', 'profile', 'cache', 'config', 'data', 'state', 'tmp']}
        for directory in directories.values():
            directory.mkdir(mode=0o700)
        current = dict(preferences)
        current.update({
            'browser.startup.page': 0,
            'browser.startup.homepage': 'about:blank',
            'browser.sessionstore.resume_from_crash': False,
            'browser.sessionstore.resume_session_once': False,
            'browser.shell.checkDefaultBrowser': False,
            'browser.download.folderList': 2,
            'browser.download.dir': str(downloads),
            'browser.cache.disk.enable': False,
        })
        content = ''.join('user_pref(' + json.dumps(key) + ', ' + json.dumps(value) + ');\n' for key, value in current.items())
        settings = directories['profile'] / 'user.js'
        settings.write_text(content, encoding='utf-8')
        settings.chmod(0o600)
        if language_pack:
            extensions = directories['profile'] / 'extensions'
            extensions.mkdir(mode=0o700)
            shutil.copyfile(language_pack, extensions / 'langpack-zh-CN@firefox.mozilla.org.xpi')
        return session, directories
    except BaseException:
        remove_session(session, runtime)
        raise


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--runtime-dir', required=True, type=Path)
    parser.add_argument('--preferences', required=True, type=Path)
    parser.add_argument('--downloads-dir', required=True, type=Path)
    parser.add_argument('--language-pack', type=Path)
    parser.add_argument('--start-url', default='about:blank')
    args = parser.parse_args()
    os.umask(0o077)
    if args.runtime_dir.is_symlink():
        raise RuntimeError('Runtime directory must not be a symbolic link')
    runtime = args.runtime_dir.resolve(strict=True)
    if runtime.stat().st_uid != os.getuid() or runtime.stat().st_mode & 0o077:
        raise RuntimeError('Runtime directory must be privately owned by the browser user')
    preferences = json.loads(args.preferences.read_text(encoding='utf-8'))
    if not isinstance(preferences, dict) or any(not re.fullmatch(r'[a-zA-Z0-9_.-]+', key) or not isinstance(value, (str, int, bool)) for key, value in preferences.items()):
        raise RuntimeError('Unexpected browser preference format')
    downloads = args.downloads_dir.resolve(strict=True)
    lock = os.open(runtime / 'launcher.lock', os.O_CREAT | os.O_RDWR | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    marker = runtime / 'active-session.json'
    marker.unlink(missing_ok=True)
    for stale in runtime.glob('session-*'):
        remove_session(stale, runtime)
    stopping = False

    def request_stop(signum, frame):
        nonlocal stopping
        stopping = True

    for signum in [signal.SIGTERM, signal.SIGINT, signal.SIGHUP]:
        signal.signal(signum, request_stop)
    desktop = subprocess.Popen(['/usr/bin/openbox'], start_new_session=True)
    try:
        while not stopping:
            session, directories = prepare_session(runtime, preferences, downloads, args.language_pack)
            browser = None
            try:
                environment = dict(os.environ)
                environment.update({
                    'HOME': str(directories['home']),
                    'XDG_CACHE_HOME': str(directories['cache']),
                    'XDG_CONFIG_HOME': str(directories['config']),
                    'XDG_DATA_HOME': str(directories['data']),
                    'XDG_STATE_HOME': str(directories['state']),
                    'TMPDIR': str(directories['tmp']),
                })
                browser = subprocess.Popen(['/usr/bin/firefox', '--no-remote', '--profile', str(directories['profile']), args.start_url], env=environment, cwd=directories['home'], start_new_session=True)
                active = {'mode': 'ephemeral', 'session_id': session.name, 'profile': str(directories['profile']), 'directory': str(session), 'firefox_pid': browser.pid, 'started_at': datetime.now(timezone.utc).isoformat()}
                pending = runtime / 'active-session.pending'
                pending.write_text(json.dumps(active) + '\n', encoding='utf-8')
                pending.chmod(0o600)
                os.replace(pending, marker)
                print(json.dumps({'event': 'ephemeral_session_started', 'session_id': session.name}), flush=True)
                while browser.poll() is None and not stopping:
                    time.sleep(0.25)
            finally:
                terminate(browser)
                marker.unlink(missing_ok=True)
                remove_session(session, runtime)
                print(json.dumps({'event': 'ephemeral_session_removed', 'session_id': session.name}), flush=True)
            if not stopping:
                time.sleep(1)
    finally:
        terminate(desktop)
        marker.unlink(missing_ok=True)
        os.close(lock)


if __name__ == '__main__':
    main()
