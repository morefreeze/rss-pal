import json
import unittest
from dataclasses import FrozenInstanceError
from unittest.mock import patch
import urllib.error

from components import Component, ProbeResult, _NoRedirectHandler, load_components, probe


class FakeResponse:
    def __init__(self, status, body=b"{}"):
        self.status = status
        self._body = body
        self.read_calls = 0
        self.closed = False

    def read(self):
        self.read_calls += 1
        return self._body

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
        self.assertEqual(response.read_calls, 1)
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
        self.assertEqual(response.read_calls, 1)
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


if __name__ == "__main__":
    unittest.main()
