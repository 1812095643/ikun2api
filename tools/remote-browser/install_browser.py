import hashlib
import json
import os
import subprocess
import tarfile
import tempfile
import urllib.request
from pathlib import Path

root = Path('/opt/ikun2api-remote-browser')
root.mkdir(parents=True, exist_ok=True)
key_path = Path('/etc/apt/keyrings/packages.mozilla.org.asc')
key_path.parent.mkdir(parents=True, exist_ok=True)
request = urllib.request.Request('https://packages.mozilla.org/apt/repo-signing-key.gpg', headers={'User-Agent': 'remote-browser-deployment'})
key = urllib.request.urlopen(request, timeout=30).read()
candidate = root / 'mozilla-key.candidate'
candidate.write_bytes(key)
process = subprocess.run(['gpg','--batch','--show-keys','--with-colons',str(candidate)], capture_output=True, text=True, check=True)
fingerprints = [line.split(':')[9] for line in process.stdout.splitlines() if line.startswith('fpr:')]
expected = '35BAA0B33E9EB396F59CA838C0BA5CE6DC6315A3'
if not fingerprints or fingerprints[0] != expected:
    raise RuntimeError('Mozilla signing key fingerprint mismatch')
key_path.write_bytes(key)
key_path.chmod(0o644)
Path('/etc/apt/sources.list.d/ikun2api-firefox.list').write_text('deb [signed-by=/etc/apt/keyrings/packages.mozilla.org.asc] https://packages.mozilla.org/apt mozilla main\n')
Path('/etc/apt/preferences.d/ikun2api-firefox').write_text('Package: firefox*\nPin: origin packages.mozilla.org\nPin-Priority: 1000\n\nPackage: firefox*\nPin: release o=Ubuntu*\nPin-Priority: -1\n')
environment = dict(os.environ)
environment['DEBIAN_FRONTEND'] = 'noninteractive'
environment['NEEDRESTART_MODE'] = 'l'
with (root / 'firefox-install.log').open('w') as log:
    subprocess.run(['apt-get','update'], env=environment, stdout=log, stderr=subprocess.STDOUT, check=True)
    subprocess.run(['apt-get','install','-y','--no-install-recommends','firefox'], env=environment, stdout=log, stderr=subprocess.STDOUT, check=True)
version = subprocess.check_output(['/usr/bin/firefox','--version'], text=True).strip()
release_request = urllib.request.Request('https://api.github.com/repos/novnc/noVNC/releases/latest', headers={'User-Agent':'remote-browser-deployment'})
release = json.load(urllib.request.urlopen(release_request, timeout=30))
tag = release['tag_name']
if release.get('prerelease') or not tag.startswith('v') or not all(part.isdigit() for part in tag[1:].split('.')):
    raise RuntimeError('Expected a stable noVNC release')
archive_path = root / ('novnc-' + tag + '.tar.gz')
archive_request = urllib.request.Request('https://codeload.github.com/novnc/noVNC/tar.gz/refs/tags/' + tag, headers={'User-Agent':'remote-browser-deployment'})
with urllib.request.urlopen(archive_request, timeout=45) as response, archive_path.open('wb') as target:
    while block := response.read(1024 * 1024):
        target.write(block)
with tarfile.open(archive_path) as archive:
    members = archive.getmembers()
    prefixes = {member.name.split('/')[0] for member in members}
    if len(prefixes) != 1:
        raise RuntimeError('Unexpected noVNC archive layout')
    archive.extractall(root, filter='data')
    web_root = root / next(iter(prefixes))
link = root / 'web'
if link.is_symlink():
    link.unlink()
elif link.exists():
    raise RuntimeError('Refusing to replace a non-symlink web root')
link.symlink_to(web_root, target_is_directory=True)
manifest = {'firefox_version':version,'firefox_package_origin':'packages.mozilla.org','mozilla_signing_key':expected,'novnc_version':tag,'novnc_published_at':release['published_at'],'novnc_archive_sha256':hashlib.file_digest(archive_path.open('rb'),'sha256').hexdigest(),'web_root':str(web_root)}
(root / 'browser-provenance.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps(manifest))
