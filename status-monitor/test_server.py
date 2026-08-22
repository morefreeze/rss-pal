import concurrent.futures
import http.client
import json
import sqlite3
import tempfile
import threading
import time
import unittest
from contextlib import closing
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
        self.services = []

    def tearDown(self):
        for service in reversed(self.services):
            close = getattr(service, "close", None)
            if close is not None:
                close()
        self.tempdir.cleanup()

    def service(
        self,
        probe_fn=None,
        db_path=None,
        now_fn=None,
        probe_timeout_seconds=None,
    ):
        service_type = getattr(server, "MonitorService", None)
        self.assertIsNotNone(service_type, "MonitorService must be defined")
        service = service_type(
            db_path=db_path or self.db_path,
            component_defs=REAL_COMPONENTS,
            probe_fn=probe_fn or (lambda _component: ProbeResult("up", 200, 5, None)),
            interval_seconds=60,
            now_fn=now_fn or (lambda: self.now),
            **(
                {"probe_timeout_seconds": probe_timeout_seconds}
                if probe_timeout_seconds is not None
                else {}
            ),
        )
        server.init_db(service.db_path)
        self.services.append(service)
        return service

    def rows(self):
        with closing(sqlite3.connect(self.db_path)) as conn:
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

        with patch("server.ThreadPoolExecutor", CapturingExecutor):
            service = self.service()
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

    def test_payload_uses_one_read_snapshot_across_all_component_queries(self):
        service = self.service()
        service.sample_once(self.now - timedelta(minutes=1))
        with closing(sqlite3.connect(self.db_path)) as conn:
            conn.execute("PRAGMA journal_mode = WAL")

        first_query_finished = threading.Event()
        writer_committed = threading.Event()
        writer_errors = []

        def writer():
            try:
                self.assertTrue(first_query_finished.wait(timeout=2))
                with closing(sqlite3.connect(self.db_path, timeout=2)) as conn:
                    conn.executemany(
                        "INSERT INTO checks "
                        "(ts, source, status, code, latency_ms, error) "
                        "VALUES (?, ?, 'down', 500, 9, 'http_error')",
                        [(self.now.isoformat(), key) for key in EXPECTED_KEYS],
                    )
                    conn.commit()
            except BaseException as exc:
                writer_errors.append(exc)
            finally:
                writer_committed.set()

        original_summary = server.aggregation.component_summary
        query_count = 0

        def coordinated_summary(*args, **kwargs):
            nonlocal query_count
            result = original_summary(*args, **kwargs)
            query_count += 1
            if query_count == 1:
                first_query_finished.set()
                self.assertTrue(writer_committed.wait(timeout=2))
            return result

        writer_thread = threading.Thread(target=writer)
        writer_thread.start()
        try:
            with patch("aggregation.component_summary", coordinated_summary):
                payload = service.payload(self.now)
        finally:
            first_query_finished.set()
            writer_thread.join(timeout=2)

        self.assertFalse(writer_thread.is_alive())
        self.assertEqual(writer_errors, [])
        statuses = {item["current_status"] for item in payload["components"]}
        self.assertIn(statuses, ({"up"}, {"down"}))

    def test_hanging_probe_hits_cycle_deadline_without_blocking_other_rows(self):
        release_hung_probe = threading.Event()
        hung_probe_finished = threading.Event()

        def probe_fn(component):
            if component.key == "api":
                try:
                    release_hung_probe.wait(timeout=2)
                finally:
                    hung_probe_finished.set()
                return ProbeResult("up", 200, 1, None)
            return ProbeResult("up", 204, 2, None)

        service = self.service(probe_fn=probe_fn, probe_timeout_seconds=0.05)
        started = time.monotonic()
        try:
            service.sample_once(self.now)
            elapsed = time.monotonic() - started
            rows = self.rows()
            self.assertLess(elapsed, 0.3)
            self.assertEqual(len(rows), 6)
            by_source = {row[0]: row[1:] for row in rows}
            self.assertEqual(
                by_source["api"], ("down", None, None, "connection_timeout")
            )
            self.assertEqual(by_source["worker"], ("up", 204, 2, None))
        finally:
            release_hung_probe.set()
        self.assertTrue(hung_probe_finished.wait(timeout=1))

    def test_repeated_cycles_do_not_resubmit_a_still_running_component(self):
        release_api = threading.Event()
        api_finished = threading.Event()
        calls = {key: 0 for key in EXPECTED_KEYS if key != "status-monitor"}
        calls_lock = threading.Lock()

        def probe_fn(component):
            with calls_lock:
                calls[component.key] += 1
            if component.key == "api":
                try:
                    release_api.wait(timeout=2)
                finally:
                    api_finished.set()
            return ProbeResult("up", 200, 1, None)

        service = self.service(probe_fn=probe_fn, probe_timeout_seconds=0.03)
        try:
            for offset in range(3):
                service.sample_once(self.now + timedelta(minutes=offset))

            self.assertEqual(calls["api"], 1)
            self.assertEqual(calls["worker"], 3)
            self.assertEqual(service.inflight_count, 1)
            api_rows = [row for row in self.rows() if row[0] == "api"]
            self.assertEqual([row[1] for row in api_rows], ["down"] * 3)
            self.assertEqual([row[4] for row in api_rows], ["connection_timeout"] * 3)

            release_api.set()
            self.assertTrue(api_finished.wait(timeout=1))
            service.sample_once(self.now + timedelta(minutes=3))

            self.assertEqual(calls["api"], 2)
            self.assertEqual(calls["worker"], 4)
            self.assertEqual(service.inflight_count, 0)
            latest_api = [row for row in self.rows() if row[0] == "api"][-1]
            self.assertEqual(latest_api[1], "up")
        finally:
            release_api.set()

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

    def test_malformed_persisted_timestamp_returns_sanitized_500(self):
        service = self.service()
        malformed = "2026-08-22T12:http://api:8080?token=secret"
        with closing(sqlite3.connect(self.db_path)) as conn:
            conn.execute(
                "INSERT INTO checks (ts, source, status) VALUES (?, 'api', 'up')",
                (malformed,),
            )
            conn.commit()

        status, _, body = self.request(service)

        self.assertEqual(status, 500)
        self.assertEqual(json.loads(body), {"error": "status_data_unavailable"})
        self.assertNotIn(malformed, body.decode())
        self.assertNotIn("api:8080", body.decode())
        self.assertNotIn(self.db_path, body.decode())

    def test_init_db_adds_composite_index_without_losing_existing_schema_or_rows(self):
        with closing(sqlite3.connect(self.db_path)) as conn:
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

        with closing(sqlite3.connect(self.db_path)) as conn:
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
        with closing(sqlite3.connect(self.db_path)) as conn:
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

        with closing(sqlite3.connect(self.db_path)) as conn:
            retained = [
                row[0]
                for row in conn.execute(
                    "SELECT source FROM checks WHERE source LIKE '%-old' "
                    "OR source IN ('at-cutoff', 'inside-window') ORDER BY id"
                )
            ]
        self.assertEqual(retained, ["at-cutoff", "inside-window"])

    def test_bind_failure_closes_service_without_starting_sampler(self):
        env = {
            "DB_PATH": str(Path(self.tempdir.name) / "startup.db"),
            "CHECK_INTERVAL": "60",
            "MONITOR_PORT": "8090",
            "FRONTEND_URL": REAL_COMPONENTS[0].url,
            "API_HEALTH_URL": REAL_COMPONENTS[1].url,
            "WORKER_HEALTH_URL": REAL_COMPONENTS[2].url,
            "RSSHUB_HEALTH_URL": REAL_COMPONENTS[3].url,
            "PUBLIC_HEALTH_URL": REAL_COMPONENTS[4].url,
        }
        real_service_type = server.MonitorService
        created_services = []

        def create_service(*args, **kwargs):
            service = real_service_type(*args, **kwargs)
            created_services.append(service)
            return service

        with (
            patch("server.MonitorService", side_effect=create_service),
            patch("server.HTTPServer", side_effect=OSError("address already in use")),
            patch("server.threading.Thread") as thread_type,
        ):
            try:
                with self.assertRaises(OSError):
                    server.main(env)

                thread_type.assert_not_called()
                self.assertEqual(len(created_services), 1)
                self.assertTrue(created_services[0].closed)
                self.assertEqual(created_services[0].inflight_count, 0)
            finally:
                for service in created_services:
                    service.close()


class StatusPageTests(unittest.TestCase):
    def setUp(self):
        self.page = server.HTML_PAGE

    def test_page_uses_the_72_hour_component_status_contract(self):
        self.assertIn("<title>RSS Pal Status</title>", self.page)
        self.assertIn("过去 72 小时可用情况", self.page)
        self.assertIn("所有系统运行正常", self.page)
        self.assertIn("部分系统故障", self.page)
        self.assertIn("每 60 秒刷新", self.page)
        self.assertIn("setInterval(loadData, 60000)", self.page)
        for field in (
            "generated_at",
            "refresh_interval_seconds",
            "overall_status",
            "components",
            "key",
            "name",
            "current_status",
            "uptime_pct",
            "last_check",
            "hours",
            "start",
            "end",
            "successful_checks",
            "total_checks",
            "avg_latency_ms",
            "last_error",
            "last_error_at",
        ):
            self.assertIn(field, self.page)

    def test_page_has_accessible_reusable_tooltip_and_all_input_events(self):
        self.assertEqual(self.page.count('role="tooltip"'), 1)
        for event_name in (
            "mouseenter",
            "mouseleave",
            "focus",
            "blur",
            "pointerdown",
            "keydown",
            "Escape",
        ):
            self.assertIn(event_name, self.page)
        self.assertIn("document.createElement('button')", self.page)
        self.assertIn("aria-label", self.page)
        self.assertIn("72 小时前", self.page)
        self.assertIn("现在", self.page)

    def test_page_requires_exactly_six_components_and_72_hour_buttons_per_row(self):
        self.assertIn("data.components.length !== 6", self.page)
        self.assertIn("typeof component.key", self.page)
        self.assertIn("component.hours.length !== HOURS", self.page)
        self.assertIn("grid-template-columns: repeat(72", self.page)
        self.assertIn("component.hours.forEach(hour =>", self.page)

    def test_page_removes_legacy_status_ui_and_untrusted_html_sinks(self):
        self.assertNotIn("innerHTML", self.page)
        self.assertNotIn("insertAdjacentHTML", self.page)
        for legacy in ("local-section", "domain-section", "domain_stats", "incidents-list"):
            self.assertNotIn(legacy, self.page)

    def test_page_maps_errors_and_uses_safe_text_helpers_for_payload_values(self):
        self.assertIn("function safeErrorLabel", self.page)
        self.assertIn("Object.prototype.hasOwnProperty.call", self.page)
        self.assertIn("检查失败", self.page)
        self.assertIn("function setText", self.page)
        self.assertIn("textContent", self.page)
        malicious_error = '<img src=x onerror=alert(1)>'
        self.assertNotIn(malicious_error, self.page)
        self.assertNotIn("last_error +", self.page)
        self.assertNotIn("last_error}`", self.page)

    def test_page_refresh_guard_retains_the_last_successful_render_on_failure(self):
        self.assertIn("let inFlight = false", self.page)
        self.assertIn("if (inFlight) return", self.page)
        self.assertIn("let lastSuccessfulData = null", self.page)
        self.assertIn("数据暂时无法刷新", self.page)
        self.assertIn("lastSuccessfulData", self.page)
        self.assertIn("finally", self.page)


if __name__ == "__main__":
    unittest.main()
