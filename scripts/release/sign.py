#!/usr/bin/env python3
"""Sign the complete release manifest. The private key comes only from CI secrets."""
import base64,hashlib,json,os,pathlib,re
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization
root=pathlib.Path(__file__).resolve().parents[2];folder=root/'dist/release'
key=Ed25519PrivateKey.from_private_bytes(base64.b64decode(os.environ['IKUN_RELEASE_SIGNING_KEY']))
public=base64.b64encode(key.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw)).decode()
expected=re.search(r'const ReleaseSigningPublicKey = "([^"]+)"',(root/'backend/internal/service/release_signature.go').read_text(encoding='utf8')).group(1)
if public!=expected:raise SystemExit('Signing key does not match the updater public key')
files=[p for p in folder.iterdir() if p.is_file() and p.name not in ('checksums.txt','checksums.txt.sig')]
manifest=''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in sorted(files))
(folder/'checksums.txt').write_bytes(manifest.encode())
(folder/'checksums.txt.sig').write_bytes(base64.b64encode(key.sign(manifest.encode())))
print(json.dumps({'signed_assets':len(files),'manifest_sha256':hashlib.sha256(manifest.encode()).hexdigest()}))
