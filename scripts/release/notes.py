#!/usr/bin/env python3
"""Render maintained release notes with installation instructions."""
import os
from pathlib import Path

version = os.environ['VERSION']
repo = os.environ['GITHUB_REPOSITORY']
ghcr = os.environ['GHCR_IMAGE']
hub = os.environ['DOCKERHUB_IMAGE']
notes = Path(f'changelog/{version}/CHANGELOG.md').read_text(encoding='utf-8').strip()
if notes.startswith('# '):
    notes = notes.split('\n', 1)[1].strip()
if not notes:
    raise SystemExit('Release notes must contain a body')
body = f'''> IKUN2api - AI API Gateway Platform \xb7 \u5c06 AI \u8ba2\u9605\u914d\u989d\u5206\u53d1\u548c\u7ba1\u7406

{notes}

---

## \U0001f4e5 Installation

**Docker (amd64 / arm64):**
```bash
# Docker Hub
docker pull {hub}:{version}

# GitHub Container Registry
docker pull {ghcr}:{version}
```

**One-line install (Linux):**
```bash
curl -fsSL https://raw.githubusercontent.com/{repo}/main/deploy/install.sh | sudo bash
```

**Manual download:**
\u4e0b\u8f7d\u4e0b\u65b9\u5bf9\u5e94\u5e73\u53f0\u7684 `ikun2api` \u5b89\u88c5\u5305\uff0c\u4f7f\u7528 `checksums.txt` \u6821\u9a8c\u5b8c\u6574\u6027\u3002

## \U0001f4da Documentation

- [GitHub Repository](https://github.com/{repo})
- [Installation Guide](https://github.com/{repo}/blob/main/deploy/README.md)

<details>
<summary>Build information</summary>

- Commit: `{os.environ['COMMIT']}`
- GHCR: `{ghcr}@{os.environ['IMAGE_DIGEST']}`
- Docker Hub: `{hub}@{os.environ['IMAGE_DIGEST']}`

</details>
'''
Path('dist/release-notes.md').write_text(body,encoding='utf-8')
