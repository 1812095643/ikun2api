#!/usr/bin/env python3
"""Reject release builds with missing or incomplete frontend output."""
import argparse
import hashlib
import json
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urljoin, urlsplit

class PageAssets(HTMLParser):
    def __init__(self):
        super().__init__()
        self.refs = []
        self.scripts = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'script' and attrs.get('src'):
            self.refs.append(attrs['src'])
            self.scripts.append(attrs['src'])
        if tag == 'link' and attrs.get('href') and attrs.get('rel') in ('stylesheet', 'modulepreload', 'icon'):
            self.refs.append(attrs['href'])

def local_asset(page, ref):
    parsed = urlsplit(ref)
    if parsed.scheme or parsed.netloc or ref.startswith('data:'):
        return None
    return unquote(urlsplit(urljoin('/' + page, ref)).path)

def verify_tree(folder):
    folder = Path(folder).resolve()
    pages = []
    for page in ('index.html', 'image-studio/index.html'):
        path = folder / page
        if not path.is_file() or not path.stat().st_size:
            raise ValueError('Missing frontend entry: ' + page + '. Build frontend/ before compiling a release.')
        body = path.read_text(encoding='utf-8')
        parser = PageAssets()
        parser.feed(body)
        if not parser.scripts:
            raise ValueError('Frontend entry has no compiled scripts: ' + page)
        checked = []
        for ref in parser.refs:
            url = local_asset(page, ref)
            if url is None:
                continue
            asset = (folder / url.lstrip('/')).resolve()
            if not asset.is_relative_to(folder) or not asset.is_file() or not asset.stat().st_size:
                raise ValueError('Missing frontend asset: ' + ref + ' referenced by ' + page)
            checked.append(url)
        pages.append({'page': page, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'assets': checked})
    return {'pages': pages, 'files': sum(p.is_file() for p in folder.rglob('*'))}

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('folder', nargs='?', type=Path, default=Path(__file__).resolve().parents[2] / 'backend/internal/web/dist')
    args = parser.parse_args()
    print(json.dumps(verify_tree(args.folder), ensure_ascii=False))
