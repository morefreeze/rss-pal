import json
import http.server
import threading
import time
import unittest
from dataclasses import FrozenInstanceError
from unittest.mock import patch
import urllib.error

from components import (
    Component,
    ProbeResult,
    _NoRedirectHandler,
    _latency_ms,
    load_components,
    probe,
)


class FakeResponse:
    def __init__(self, status, body=b"{}"):
        self.status = status
        self._body = body
        self._offset = 0
        self.read_calls = 0
        self.closed = False

    def read(self, size=-1):
        self.read_calls += 1
        if size < 0:
            chunk = self._body[self._offset :]
        else:
            chunk = self._body[self._offset : self._offset + size]
        self._offset += len(chunk)
        return chunk

    def close(self):
        self.closed = True


def response_for(status, body):
    return FakeResponse(status, body)


class ComponentProbeTests(unittest.TestCase):
    def setUp(self):
        self.api = Component("api", "API", "http://api/health", "json_ok")
        self.frontend = Component("frontend", "Frontend", "http://frontend/", "http")
        self.rsshub = Component("rsshub", "RSSHub", "http://rsshub/health", "http")

    def test_api_requires_200_json_ok(self):
        ok_response = response_for(200, json.dumps({"status": "ok"}).encode())
        with patch("components._NO_REDIRECT_OPENER.open", return_value=ok_response):
            result = probe(self.api)
        self.assertEqual(result, ProbeResult("up", 200, result.latency_ms, None))

        down_response = response_for(200, json.dumps({"status": "down"}).encode())
        with patch("components._NO_REDIRECT_OPENER.open", return_value=down_response):
            result = probe(self.api)
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 200)
        self.assertEqual(result.error, "invalid_response")

    def test_api_rejects_invalid_json(self):
        response = response_for(200, b"not-json")
        with patch("components._NO_REDIRECT_OPENER.open", return_value=response):
            result = probe(self.api)
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 200)
        self.assertEqual(result.error, "invalid_response")

    def test_frontend_accepts_302(self):
        response = response_for(302, b"redirect")
        redirect_error = urllib.error.HTTPError(
            self.frontend.url, 302, "Found", {}, response
        )
        with patch("components._NO_REDIRECT_OPENER.open", side_effect=redirect_error):
            result = probe(self.frontend)
        self.assertEqual(result.status, "up")
        self.assertEqual(result.code, 302)
        self.assertIsNone(result.error)
        self.assertGreaterEqual(response.read_calls, 1)
        self.assertTrue(response.closed)

    def test_rsshub_rejects_500(self):
        response = response_for(500, b"server error")
        with patch("components._NO_REDIRECT_OPENER.open", return_value=response):
            result = probe(self.rsshub)
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 500)
        self.assertEqual(result.error, "http_error")

    def test_timeout_is_down_and_sanitized(self):
        with patch("components._NO_REDIRECT_OPENER.open", side_effect=TimeoutError("slow")):
            result = probe(self.api)
        self.assertEqual(result.status, "down")
        self.assertIsNone(result.code)
        self.assertEqual(result.error, "connection_timeout")
        self.assertIsInstance(result.latency_ms, int)

    def test_error_never_contains_url_credentials_or_traceback(self):
        error = RuntimeError(
            "GET https://user:password@api.internal:8443/health failed\n"
            "Traceback (most recent call last): SELECT secret FROM users"
        )
        with patch("components._NO_REDIRECT_OPENER.open", side_effect=error):
            result = probe(self.api)
        self.assertEqual(result, ProbeResult("down", None, result.latency_ms, "connection_failed"))
        self.assertNotIn("api.internal", result.error)
        self.assertNotIn("password", result.error)
        self.assertNotIn("Traceback", result.error)
        self.assertNotIn("SELECT", result.error)

    def test_response_body_is_drained_and_closed(self):
        response = response_for(200, json.dumps({"status": "ok"}).encode())
        with patch("components._NO_REDIRECT_OPENER.open", return_value=response):
            result = probe(self.api)
        self.assertEqual(result.status, "up")
        self.assertGreaterEqual(response.read_calls, 1)
        self.assertTrue(response.closed)

    def test_components_are_immutable(self):
        with self.assertRaises(FrozenInstanceError):
            self.api.url = "http://changed/"

    def test_redirect_handler_does_not_follow_redirects(self):
        handler = _NoRedirectHandler()
        self.assertIsNone(
            handler.redirect_request(None, None, 302, "Found", {}, "http://elsewhere/")
        )

    def test_load_components_order_names_and_probe_kinds(self):
        env = {
            "FRONTEND_URL": "http://frontend/",
            "API_HEALTH_URL": "http://api/health",
            "WORKER_HEALTH_URL": "http://api/internal/health/worker",
            "RSSHUB_HEALTH_URL": "http://rsshub/health",
            "PUBLIC_HEALTH_URL": "https://rss.morefreeze.top/api/health",
        }
        components = load_components(env)
        self.assertEqual(
            components,
            (
                Component("frontend", "Frontend", env["FRONTEND_URL"], "http"),
                Component("api", "API", env["API_HEALTH_URL"], "json_ok"),
                Component("worker", "Worker", env["WORKER_HEALTH_URL"], "json_ok"),
                Component("rsshub", "RSSHub", env["RSSHUB_HEALTH_URL"], "http"),
                Component("public", "公网入口", env["PUBLIC_HEALTH_URL"], "json_ok"),
            ),
        )

    def test_latency_conversion_uses_monotonic_milliseconds(self):
        with patch("components._monotonic", return_value=123.456):
            self.assertEqual(_latency_ms(123.000), 456)


class _HealthHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        if self.path == "/redirect":
            self.send_response(302)
            self.send_header("Location", "/target")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if self.path == "/target":
            self._send(200, b"target")
            return
        if self.path == "/error":
            self._send(500, b"error")
            return
        if self.path == "/json-non200":
            self._send(503, b'{"status":"ok"}')
            return
        if self.path == "/oversized":
            self._send(200, b"x" * (64 * 1024 + 1))
            return
        if self.path == "/oversized-http":
            self._send(200, b"x" * (64 * 1024 + 1))
            return
        if self.path == "/oversized-json-error":
            self._send(503, b"x" * (64 * 1024 + 1))
            return
        if self.path == "/drip":
            body = b"abc"
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            time.sleep(0.04)
            try:
                for byte in body:
                    self.wfile.write(bytes([byte]))
                    self.wfile.flush()
                    time.sleep(0.03)
            except BrokenPipeError:
                pass
            return
        self._send(404, b"missing")

    def _send(self, code, body):
        self.send_response(code)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass


class LoopbackProbeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), _HealthHandler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.server.server_port}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=2)

    def component(self, path, kind="http"):
        return Component("loopback", "Loopback", self.base_url + path, kind)

    def test_real_opener_suppresses_redirect_and_accepts_original_302(self):
        result = probe(self.component("/redirect"))
        self.assertEqual(result, ProbeResult("up", 302, result.latency_ms, None))

    def test_real_500_is_http_error(self):
        result = probe(self.component("/error"))
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 500)
        self.assertEqual(result.error, "http_error")

    def test_real_json_probe_rejects_non_200(self):
        result = probe(self.component("/json-non200", "json_ok"))
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 503)
        self.assertEqual(result.error, "http_error")

    def test_real_oversized_body_is_invalid_response(self):
        result = probe(self.component("/oversized", "json_ok"))
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 200)
        self.assertEqual(result.error, "invalid_response")

    def test_real_oversized_http_body_remains_up(self):
        result = probe(self.component("/oversized-http"))
        self.assertEqual(result.status, "up")
        self.assertEqual(result.code, 200)
        self.assertIsNone(result.error)

    def test_real_oversized_non200_json_body_is_http_error(self):
        result = probe(self.component("/oversized-json-error", "json_ok"))
        self.assertEqual(result.status, "down")
        self.assertEqual(result.code, 503)
        self.assertEqual(result.error, "http_error")

    def test_real_drip_body_hits_overall_deadline(self):
        started = time.monotonic()
        result = probe(self.component("/drip", "json_ok"), timeout=0.06)
        elapsed = time.monotonic() - started
        self.assertEqual(result.status, "down")
        self.assertEqual(result.error, "connection_timeout")
        self.assertLess(elapsed, 0.2)


if __name__ == "__main__":
    unittest.main()
