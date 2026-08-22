import concurrent.futures
import http.client
import json
import sqlite3
import tempfile
import threading
import unittest
from datetime import datetime, timedelta
from pathlib import Path
from unittest.mock import patch

import server
from aggregation import CST
from components import Component, ProbeResult


REAL_COMPONENTS = (
    Component("frontend", "Frontend", "http://frontend/", "http"),
    Component("api", "API", "http://api/health", "json_ok"),
    Component("worker", "Worker", "http://api/internal/health/worker", "json_ok"),
    Component("rsshub", "RSSHub", "http://rsshub/health", "http"),
    Component("public", "公网入口", "https://rss.example/api/health", "json_ok"),
)
EXPECTED_KEYS = ["frontend", "api", "worker", "rsshub", "status-monitor", "public"]


class MonitorServiceTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.db_path = str(Path(self.tempdir.name) / "status.db")
        self.now = datetime(2026, 8, 22, 12, 34, 56, tzinfo=CST)

    def tearDown(self):
        self.tempdir.cleanup()

    def service(self, probe_fn=None, db_path=None, now_fn=None):
        service_type = getattr(server, "MonitorService", None)
        self.assertIsNotNone(service_type, "MonitorService must be defined")
        service = service_type(
            db_path=db_path or self.db_path,
            component_defs=REAL_COMPONENTS,
            probe_fn=probe_fn or (lambda _component: ProbeResult("up", 200, 5, None)),
            interval_seconds=60,
            now_fn=now_fn or (lambda: self.now),
        )
        server.init_db(service.db_path)
        return service

    def rows(self):
        with sqlite3.connect(self.db_path) as conn:
            return conn.execute(
                "SELECT source, status, code, latency_ms, error FROM checks ORDER BY id"
            ).fetchall()

    def request(self, service, path="/api/status"):
        handler = server.make_handler(service)
        httpd = server.HTTPServer(("127.0.0.1", 0), handler)
        thread = threading.Thread(target=httpd.serve_forever, daemon=True)
        thread.start()
        try:
            conn = http.client.HTTPConnection("127.0.0.1", httpd.server_port, timeout=2)
            conn.request("GET", path)
            response = conn.getresponse()
            body = response.read()
            headers = dict(response.getheaders())
            conn.close()
            return response.status, headers, body
        finally:
            httpd.shutdown()
            httpd.server_close()
            thread.join(timeout=2)

    def test_sample_once_records_exact_six_ordered_component_sources(self):
        service = self.service()

        service.sample_once(self.now)

        sources = [row[0] for row in self.rows()]
        self.assertEqual(sources, EXPECTED_KEYS)
        self.assertNotIn("local", sources)
        self.assertNotIn("domain", sources)
        self.assertEqual([component.key for component in service.component_defs], EXPECTED_KEYS)

    def test_real_probes_use_a_bounded_five_worker_executor(self):
        captured = []
        real_executor = concurrent.futures.ThreadPoolExecutor

        class CapturingExecutor(real_executor):
            def __init__(self, max_workers=None, *args, **kwargs):
                captured.append(max_workers)
                super().__init__(max_workers=max_workers, *args, **kwargs)

        service = self.service()
        with patch("server.ThreadPoolExecutor", CapturingExecutor):
            service.sample_once(self.now)

        self.assertEqual(captured, [5])

    def test_one_probe_exception_does_not_block_other_components_and_is_sanitized(self):
        def probe_fn(component):
            if component.key == "api":
                raise RuntimeError("GET http://api:8080?token=secret failed")
            return ProbeResult("up", 204, 7, None)

        service = self.service(probe_fn)
        service.sample_once(self.now)

        rows = self.rows()
        self.assertEqual(len(rows), 6)
        by_source = {row[0]: row[1:] for row in rows}
        self.assertEqual(by_source["api"], ("down", None, None, "connection_failed"))
        self.assertEqual(by_source["worker"], ("up", 204, 7, None))
        self.assertNotIn("api:8080", repr(rows))
        self.assertNotIn("secret", repr(rows))

    def test_untrusted_probe_result_error_never_reaches_database_payload_or_api(self):
        secret = "http://api:8080?token=secret"

        def probe_fn(component):
            if component.key == "api":
                return ProbeResult("down", None, 3, secret)
            return ProbeResult("up", 200, 3, None)

        service = self.service(probe_fn)
        service.sample_once(self.now)
        db_text = json.dumps(self.rows())
        payload_text = json.dumps(service.payload(self.now))
        status, _, api_body = self.request(service)

        self.assertEqual(status, 200)
        self.assertNotIn(secret, db_text)
        self.assertNotIn(secret, payload_text)
        self.assertNotIn(secret, api_body.decode())
        self.assertIn("connection_failed", api_body.decode())

    def test_self_health_uses_actual_completion_time_after_a_slow_cycle(self):
        first_start = self.now
        first_completion = first_start + timedelta(seconds=70)
        second_start = first_completion + timedelta(seconds=60)
        second_completion = second_start + timedelta(seconds=70)
        third_start = second_completion + timedelta(seconds=121)
        completion_times = iter(
            [first_completion, second_completion, third_start + timedelta(seconds=1)]
        )
        service = self.service(now_fn=lambda: next(completion_times))

        service.sample_once(first_start)
        service.sample_once(second_start)
        service.sample_once(third_start)

        rows = [row for row in self.rows() if row[0] == "status-monitor"]
        self.assertEqual([row[1] for row in rows], ["up", "up", "down"])
        self.assertEqual(
            service.last_cycle_completed_at,
            third_start + timedelta(seconds=1),
        )

    def test_self_health_at_exactly_120_seconds_since_completion_remains_up(self):
        first_start = self.now
        first_completion = first_start + timedelta(seconds=70)
        second_start = first_completion + timedelta(seconds=120)
        completion_times = iter([first_completion, second_start + timedelta(seconds=1)])
        exactly_service = self.service(now_fn=lambda: next(completion_times))

        exactly_service.sample_once(first_start)
        exactly_service.sample_once(second_start)

        exactly_rows = [row for row in self.rows() if row[0] == "status-monitor"]
        self.assertEqual([row[1] for row in exactly_rows], ["up", "up"])

    def test_status_api_returns_six_ordered_components_with_72_hours_each(self):
        service = self.service()
        service.sample_once(self.now)

        status, headers, body = self.request(service)
        payload = json.loads(body)

        self.assertEqual(status, 200)
        self.assertEqual(headers["Content-Type"], "application/json; charset=utf-8")
        self.assertEqual([item["key"] for item in payload["components"]], EXPECTED_KEYS)
        self.assertEqual([len(item["hours"]) for item in payload["components"]], [72] * 6)
        self.assertEqual(payload["refresh_interval_seconds"], 60)

    def test_sqlite_read_error_returns_500_without_fake_green_or_details(self):
        missing_schema = str(Path(self.tempdir.name) / "missing-schema.db")
        service = self.service(db_path=missing_schema)
        Path(missing_schema).unlink()

        status, headers, body = self.request(service)

        self.assertEqual(status, 500)
        self.assertEqual(headers["Content-Type"], "application/json; charset=utf-8")
        self.assertEqual(json.loads(body), {"error": "status_data_unavailable"})
        self.assertNotIn("overall_status", body.decode())
        self.assertNotIn(missing_schema, body.decode())

    def test_init_db_adds_composite_index_without_losing_existing_schema_or_rows(self):
        with sqlite3.connect(self.db_path) as conn:
            conn.execute(
                "CREATE TABLE checks ("
                "id INTEGER PRIMARY KEY AUTOINCREMENT, ts TEXT NOT NULL, "
                "source TEXT NOT NULL DEFAULT 'domain', status TEXT NOT NULL, "
                "code INTEGER, latency_ms INTEGER, error TEXT)"
            )
            conn.execute(
                "INSERT INTO checks (ts, source, status, code, latency_ms, error) "
                "VALUES (?, ?, ?, ?, ?, ?)",
                (self.now.isoformat(), "legacy", "up", 200, 4, None),
            )
            original_columns = conn.execute("PRAGMA table_info(checks)").fetchall()
            conn.commit()

        server.init_db(self.db_path)

        with sqlite3.connect(self.db_path) as conn:
            columns = conn.execute("PRAGMA table_info(checks)").fetchall()
            rows = conn.execute(
                "SELECT ts, source, status, code, latency_ms, error FROM checks"
            ).fetchall()
            indexes = conn.execute("PRAGMA index_list(checks)").fetchall()
            composite = []
            for index in indexes:
                names = [
                    row[2] for row in conn.execute(f'PRAGMA index_info("{index[1]}")')
                ]
                if names == ["source", "ts"]:
                    composite.append(index[1])

        self.assertEqual(columns, original_columns)
        self.assertEqual(rows, [(self.now.isoformat(), "legacy", "up", 200, 4, None)])
        self.assertEqual(len(composite), 1)

    def test_sample_cleanup_removes_only_data_older_than_seven_days(self):
        service = self.service()
        cutoff = self.now - timedelta(days=7)
        with sqlite3.connect(self.db_path) as conn:
            conn.executemany(
                "INSERT INTO checks (ts, source, status) VALUES (?, ?, 'up')",
                [
                    ((cutoff - timedelta(microseconds=1)).isoformat(), "too-old"),
                    (cutoff.isoformat(), "at-cutoff"),
                    ((cutoff + timedelta(seconds=1)).isoformat(), "inside-window"),
                ],
            )
            conn.commit()

        service.sample_once(self.now)

        with sqlite3.connect(self.db_path) as conn:
            retained = [
                row[0]
                for row in conn.execute(
                    "SELECT source FROM checks WHERE source LIKE '%-old' "
                    "OR source IN ('at-cutoff', 'inside-window') ORDER BY id"
                )
            ]
        self.assertEqual(retained, ["at-cutoff", "inside-window"])


if __name__ == "__main__":
    unittest.main()
