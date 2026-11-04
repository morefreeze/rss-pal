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

    def assert_refused_without_write(self, source):
        with tempfile.TemporaryDirectory(dir=self.path.parent) as directory:
            path = pathlib.Path(directory) / "nginx.prod.conf"
            original = source.encode("utf-8")
            path.write_bytes(original)
            path.chmod(0o640)
            inode = path.stat().st_ino
            result = self.run_helper(path)
            self.assertNotEqual(result.returncode, 0, "unsafe configuration accepted")
            self.assertEqual(path.read_bytes(), original)
            self.assertEqual(path.stat().st_ino, inode)
            self.assertEqual(path.stat().st_mode & 0o777, 0o640)
            self.assertEqual(list(path.parent.glob("*.bak")), [])
            self.assertEqual(list(path.parent.iterdir()), [path])

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

    def test_refuses_more_specific_prefix_and_matching_or_unknown_regex(self):
        for route in [
            'location ^~ /api/proxy/ { return 403; }',
            'location /api/proxy/ { return 403; }',
            'location /api/proxy/im { return 403; }',
            'location "^~" "/api/proxy/" { return 403; }',
            'location ~ ^/api/proxy/image$ { return 403; }',
            'location ~ "^/api/proxy/image$" { return 403; }',
            'location "~*" "^/API/PROXY/IMAGE$" { return 403; }',
            'location ~ "(?i)^/api/proxy/image$" { return 403; }',
            'location ~ "^/not-image$|/api/proxy/image" { return 403; }',
            'location ~ "(?i)^/unrelated$" { return 403; }',
            r'location ~ "\x2fapi/proxy/image" { return 403; }',
            r'location ~ \\x2fapi/proxy/image { return 403; }',
            'location ~ [[:alpha:]] { return 403; }',
            'include private-routes.conf;',
        ]:
            with self.subTest(route=route):
                source = self.original.replace("    # API proxy.", "    " + route + "\n\n    # API proxy.")
                # A plain generic prefix permits nginx regex locations to
                # override it; inserting an exact route must not erase that.
                source = source.replace("location ^~ /api {", "location /api {")
                self.assert_refused_without_write(source)

    def test_refuses_quoted_directive_names_recursively_before_classification(self):
        routes = [
            '"location" ^~ /api/proxy/ { return 403; }',
            "'location' ^~ /api/proxy/ { return 403; }",
            '"include" private-routes.conf;',
            "'include' private-routes.conf;",
            'location /custom { "location" /custom/private { return 403; } }',
            'location /custom { "return" 204; }',
        ]
        sources = [self.original.replace("    # API proxy.", "    " + route + "\n\n    # API proxy.") for route in routes]
        sources += [self.original + quoted + ' { location /api/proxy/ { return 403; } }\n' for quoted in ['"server"', "'server'"]]
        for source in sources:
            with self.subTest(source=source[-180:]):
                self.assert_refused_without_write(source)

    def test_refuses_proxy_pass_uri_components_before_backup(self):
        for suffix in ["/", "/base", "?query=1", "#fragment"]:
            for variable in [False, True]:
                with self.subTest(suffix=suffix, variable=variable):
                    source = self.original.replace("location ^~ /api {", "location /api/ {")
                    if variable:
                        source = source.replace("http://api:8080", "http://api:8080" + suffix)
                    else:
                        source = source.replace("        set $upstream_api http://api:8080;\n", "")
                        source = source.replace("proxy_pass $upstream_api;", "proxy_pass http://127.0.0.1:18080" + suffix + ";")
                    self.assert_refused_without_write(source)

    def test_unrelated_routes_and_origin_only_static_upstream_are_preserved(self):
        routes = '''    location /api/proxy/health { return 204; }
    location /api/proxy/image-extra { return 204; }
    location ~ "^/unrelated$" { return 204; }
    location ~ ^/api/media/ { return 204; }
    location ~* \\.(png|css)$ { return 404; }
    location "~*" "^/quoted-unrelated$" { return 200 "quoted argument"; }

'''
        source = self.original.replace("    # API proxy.", routes + "    # API proxy.")
        source = source.replace("        set $upstream_api http://api:8080;\n", "")
        source = source.replace("proxy_pass $upstream_api;", "proxy_pass http://127.0.0.1:18080;")
        source = source.replace("server_name localhost;", 'server_name "localhost";')
        self.path.write_text(source)
        result = self.run_helper(self.path)
        self.assertEqual(result.returncode, 0, result.stderr)
        updated = self.path.read_text()
        exact = updated[updated.index("    location = /api/proxy/image {"):updated.index("    location ^~ /api {")]
        self.assertEqual(updated.replace(exact, ""), source)
        self.assertIn("proxy_pass http://127.0.0.1:18080;", exact)
        again = self.run_helper(self.path)
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertEqual(again.stdout, "")

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
