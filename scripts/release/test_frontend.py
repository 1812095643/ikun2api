#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path
from verify_frontend import verify_tree

class FrontendReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for page, base in [('index.html', '/assets/'), ('image-studio/index.html', '/image-studio/static/')]:
            path = self.root/page
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(f'<html><script type="module" src="{base}main.js"></script><link rel="stylesheet" href="{base}main.css"></html>')
            folder = self.root/base.lstrip('/')
            folder.mkdir(parents=True, exist_ok=True)
            (folder/'main.js').write_text('console.log(1)')
            (folder/'main.css').write_text('body{color:red}')
    def test_complete_frontend(self):
        self.assertEqual(verify_tree(self.root)['files'], 6)
    def test_missing_entry_rejected(self):
        (self.root/'index.html').unlink()
        with self.assertRaisesRegex(ValueError, 'Missing frontend entry'):
            verify_tree(self.root)
    def test_missing_lazy_or_style_asset_rejected(self):
        (self.root/'assets/main.css').unlink()
        with self.assertRaisesRegex(ValueError, 'Missing frontend asset'):
            verify_tree(self.root)
    def test_missing_studio_rejected(self):
        (self.root/'image-studio/index.html').unlink()
        with self.assertRaisesRegex(ValueError, 'image-studio'):
            verify_tree(self.root)
    def test_placeholder_entry_rejected(self):
        (self.root/'index.html').write_text('<html>frontend placeholder</html>')
        with self.assertRaisesRegex(ValueError, 'no compiled scripts'):
            verify_tree(self.root)

if __name__ == '__main__':
    unittest.main()
