#!/usr/bin/env python3
"""Exercise the deployment helper against the actual single-file bind source."""
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
HELPER = ROOT / "scripts/ensure_image_proxy_nginx.py"
FIXTURE = ROOT / "scripts/tests/fixtures/nginx.prod.before-image-timeout.conf"


class ImageProxyNginxTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = pathlib.Path(self.directory.name) / "nginx.prod.conf"
        self.original = FIXTURE.read_text()
        self.path.write_text(self.original)
        self.path.chmod(0o640)

    def run_helper(self, *args, stdin=None):
        return subprocess.run(["python3", str(HELPER), *map(str, args)], input=stdin, capture_output=True, text=True)

    def test_actual_mounted_template_backup_atomic_idempotent_and_restore(self):
        inode = self.path.stat().st_ino
        result = self.run_helper(self.path)
        self.assertEqual(result.returncode, 0, result.stderr)
        backup = pathlib.Path(result.stdout.strip())
        self.assertEqual(backup.read_text(), self.original)
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o640)
        self.assertNotEqual(inode, self.path.stat().st_ino)
        updated = self.path.read_text()
        exact = updated[updated.index("    location = /api/proxy/image {"):updated.index("    location ^~ /api {")]
        self.assertIn("proxy_pass $upstream_api;", exact)
        self.assertIn("proxy_set_header Host $host;", exact)
        self.assertIn("proxy_read_timeout 120s;", exact)
        self.assertEqual(updated.replace(exact, ""), self.original)
        again = self.run_helper(self.path)
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stdout, "")
        self.assertEqual(len(list(self.path.parent.glob("*.bak"))), 1)
        self.assertEqual(self.run_helper("--check-stdin", stdin=updated).returncode, 0)
        self.assertNotEqual(self.run_helper("--check-stdin", stdin=self.original).returncode, 0)
        restored = self.run_helper("--restore", backup, self.path)
        self.assertEqual(restored.returncode, 0, restored.stderr)
        self.assertEqual(self.path.read_text(), self.original)

    def test_custom_upstream_headers_and_unrelated_routes_are_preserved(self):
        self.original = self.original.replace("http://api:8080", "http://private-api:18080").replace(
            "proxy_set_header Host $host;", "proxy_set_header Host internal.example;"
        ).replace("    # SPA fallback.", "    location = /custom { return 204; }\n\n    # SPA fallback.")
        self.path.write_text(self.original)
        result = self.run_helper(self.path)
        self.assertEqual(result.returncode, 0, result.stderr)
        updated = self.path.read_text()
        exact = updated[updated.index("    location = /api/proxy/image {"):updated.index("    location ^~ /api {")]
        self.assertIn("http://private-api:18080", exact)
        self.assertIn("proxy_set_header Host internal.example;", exact)
        self.assertEqual(updated.replace(exact, ""), self.original)

    def test_existing_recognized_image_location_changes_only_timeout(self):
        self.assertEqual(self.run_helper(self.path).returncode, 0)
        valid = self.path.read_text()
        stale = valid.replace("proxy_read_timeout 120s;", "proxy_read_timeout 60s;", 1)
        self.path.write_text(stale)
        result = self.run_helper(self.path)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.path.read_text(), valid)
        self.assertEqual(pathlib.Path(result.stdout.strip()).read_text(), stale)

    def test_loaded_nginx_dump_and_tracked_multihost_config(self):
        tracked = (ROOT / "frontend/nginx.conf").read_text()
        dump = "# configuration file /etc/nginx/nginx.conf:\nhttp { include /etc/nginx/conf.d/*.conf; }\n" + tracked
        result = self.run_helper("--check-stdin", stdin=dump)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_refuses_unknown_ambiguous_or_symlink_without_overwrite(self):
        cases = [
            self.original.replace("location ^~ /api {", "location ^~ /unknown {"),
            self.original.replace("proxy_read_timeout 60s;", "include hidden-api.conf;"),
            self.original.replace("location ^~ /api {", "location ^~ /api { if ($request_method = POST) { return 403; }"),
            self.original.replace("proxy_pass $upstream_api;", "proxy_pass $upstream_api; proxy_pass http://another;"),
            self.original + "server { location /api { proxy_pass http://ambiguous; } }",
            self.original.replace("location ^~ /api {", "location = /api/proxy/image { return 404; }\n    location ^~ /api {"),
        ]
        for source in cases:
            with self.subTest(source=source[-120:]):
                self.path.write_text(source)
                result = self.run_helper(self.path)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.path.read_text(), source)
                self.assertEqual(list(self.path.parent.glob("*.bak")), [])
        self.path.unlink()
        target = self.path.parent / "target"
        target.write_text(self.original)
        self.path.symlink_to(target)
        self.assertNotEqual(self.run_helper(self.path).returncode, 0)
        self.assertEqual(target.read_text(), self.original)


if __name__ == "__main__":
    unittest.main()
