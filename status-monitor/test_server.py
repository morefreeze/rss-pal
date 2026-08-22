import concurrent.futures
import http.client
import json
import re
import sqlite3
import subprocess
import tempfile
import textwrap
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


NODE_PAGE_HARNESS = r"""
const fs = require('fs');
const vm = require('vm');
const script = fs.readFileSync(0, 'utf8');

class Node {
  constructor(tag = 'div', document = null) { this.tagName = tag.toUpperCase(); this.ownerDocument = document; this.children = []; this.parentNode = null; this.className = ''; this.hidden = false; this.style = {}; this.attributes = {}; this.listeners = {}; this._text = ''; this.tabIndex = -1; this.scrollLeft = 0; this.rect = { left: 20, top: 30, bottom: 48, width: 320, height: 120 }; }
  set textContent(value) { this._text = String(value); this.children = []; }
  get textContent() { return this._text + this.children.map(child => child.textContent).join(''); }
  appendChild(child) { if (child.tagName === '#FRAGMENT') { [...child.children].forEach(item => this.appendChild(item)); child.children = []; return child; } child.parentNode = this; this.children.push(child); return child; }
  append(...children) { children.forEach(child => this.appendChild(child)); }
  contains(node) { return node === this || this.children.some(child => child.contains(node)); }
  replaceChildren(...children) { if (this.ownerDocument && this.contains(this.ownerDocument.activeElement)) this.ownerDocument.activeElement = null; this.children = []; this._text = ''; children.forEach(child => this.appendChild(child)); }
  setAttribute(name, value) { this.attributes[name] = String(value); }
  getAttribute(name) { return this.attributes[name] ?? null; }
  addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
  dispatch(name, event = {}) { (this.listeners[name] || []).forEach(handler => handler({ type: name, preventDefault() { this.defaultPrevented = true; }, ...event })); }
  focus(options = {}) { const prior = this.ownerDocument.activeElement; if (prior && prior !== this) prior.dispatch('blur'); if (!options.preventScroll) { let ancestor = this.parentNode; while (ancestor && ancestor.className !== 'hour-scroller') ancestor = ancestor.parentNode; if (ancestor) ancestor.scrollLeft = 999; } this.ownerDocument.activeElement = this; this.dispatch('focus'); }
  getBoundingClientRect() { return this.rect; }
}
class Document {
  constructor() { this.byId = {}; this.listeners = {}; this.activeElement = null; }
  createElement(tag) { return new Node(tag, this); }
  createDocumentFragment() { return new Node('#fragment', this); }
  getElementById(id) { return this.byId[id]; }
  addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
  dispatch(name, event = {}) { (this.listeners[name] || []).forEach(handler => handler({ type: name, ...event })); }
}
function makePayload({ nullLastCheck = false, malicious = false } = {}) {
  const statusPatterns = [
    ['down', 'up', null], ['up', 'down', null], ['down', null, 'up'],
    ['up', null, 'down'], [null, 'down', 'up'], [null, 'up', 'down'],
  ];
  return { generated_at: '2026-08-22T12:00:00+08:00', refresh_interval_seconds: 60, overall_status: 'up',
    components: Array.from({ length: 6 }, (_, componentIndex) => {
      const hours = Array.from({ length: 72 }, (_, hourIndex) => {
        const status = statusPatterns[componentIndex][hourIndex % statusPatterns[componentIndex].length];
        const fixedDownHour = componentIndex === 0 && hourIndex === 0;
        return {
          start: fixedDownHour ? '2025-01-02T03:04:05+08:00' : `2026-08-${20 + componentIndex}T${String(hourIndex % 24).padStart(2, '0')}:00:00+08:00`,
          end: fixedDownHour ? '2025-01-02T04:05:06+08:00' : `2026-08-${20 + componentIndex}T${String((hourIndex + 1) % 24).padStart(2, '0')}:00:00+08:00`,
          status,
          uptime_pct: status == null ? null : status === 'down' ? 0 : 100,
          successful_checks: status === 'up' ? 1 : 0,
          total_checks: status == null ? 0 : 1,
          avg_latency_ms: status == null ? null : 12 + componentIndex,
          last_error: status === 'down' ? (fixedDownHour && malicious ? '<img src=x onerror=alert(1)>' : 'connection_timeout') : null,
          last_error_at: status === 'down' ? (fixedDownHour ? '2025-01-02T03:30:45+08:00' : `2026-08-${20 + componentIndex}T00:30:00+08:00`) : null,
        };
      });
      return { key: `component-${componentIndex}`, name: `Component ${componentIndex}`, current_status: componentIndex % 2 ? 'down' : 'up', uptime_pct: 100 - componentIndex, last_check: nullLastCheck && componentIndex === 0 ? null : '2026-08-22T12:00:00+08:00', hours };
    }) };
}
function page(fetchImpl) {
  const document = new Document();
  for (const id of ['status-tooltip', 'components', 'overall-banner', 'refresh-notice', 'last-update', 'refresh-label']) document.byId[id] = new Node('div', document);
  document.byId['status-tooltip'].hidden = true;
  document.byId['refresh-notice'].hidden = true;
  const timers = []; let fetchCalls = 0;
  document.byId['status-tooltip'].rect = { left: 0, top: 0, bottom: 0, width: 320, height: 120 };
  const context = { console, document, window: { innerWidth: 1024, innerHeight: 640 }, AbortController, Date, Number, Object, String, Array, Math, Error, Promise,
    fetch: (...args) => { fetchCalls += 1; return fetchImpl(...args); },
    setInterval: () => 1,
    setTimeout: callback => { timers.push(callback); return timers.length - 1; },
    clearTimeout: index => { timers[index] = null; },
  };
  context.globalThis = context;
  vm.createContext(context); vm.runInContext(script, context);
  return { context, document, timers, calls: () => fetchCalls };
}
const flush = () => new Promise(resolve => setImmediate(resolve));
const activeIsClear = context => vm.runInContext('activeTooltipButton === null', context);
const hasImage = node => node.tagName === 'IMG' || node.children.some(hasImage);
const trackFor = row => row.children[2].children[0];
const barFor = row => trackFor(row).children[0];
function assert(condition, message) { if (!condition) throw new Error(message); }

(async () => {
  let next = makePayload({ nullLastCheck: true, malicious: true });
  let fetchImpl = () => Promise.resolve({ ok: true, json: () => Promise.resolve(next) });
  const live = page((...args) => fetchImpl(...args));
  await flush(); await flush();
  const root = live.document.byId.components;
  assert(root.children.length === 6, 'must render six component rows');
  assert(next.components.slice(1).every(component => component.hours.some((hour, hourIndex) => hour.status !== next.components[0].hours[hourIndex].status)), 'fixture must make every component history differ from component 0');
  assert(root.children.map(row => row.children[0].children[0].textContent).join('|') === 'Component 0|Component 1|Component 2|Component 3|Component 4|Component 5', 'must preserve payload component order');
  assert(root.children.every((row, index) => row.children[0].children[1].textContent === (index % 2 ? '故障' : '正常') && row.children[0].children[2].textContent === `可用率 ${(100 - index).toFixed(2)}%`), 'must render current status and sample-weighted uptime');
  assert(root.children.every(row => barFor(row).children.length === 72), 'must render 72 buttons per row');
  assert(root.children.every(row => barFor(row).children.filter(control => control.tabIndex === 0).length === 1), 'each row must expose one roving tab stop');
  assert(root.children.flatMap(row => barFor(row).children).filter(control => control.tabIndex === 0).length === 6, 'six rows must expose exactly six sequential tab stops');
  assert(root.children.every(row => row.children[2].className === 'hour-scroller' && row.children[2].attributes['aria-label'].includes('横向滑动') && trackFor(row).className === 'timeline-track' && barFor(row).parentNode === trackFor(row) && trackFor(row).children[1].parentNode === trackFor(row)), 'each status bar must have a shared scroll track for aligned grid and axis');
  const rovingBar = barFor(root.children[0]); const newest = rovingBar.children[71];
  assert(newest.tabIndex === 0, 'newest hour must be the initial row tab stop'); newest.focus(); newest.dispatch('keydown', { key: 'ArrowLeft' }); assert(live.document.activeElement === rovingBar.children[70] && rovingBar.children[70].tabIndex === 0 && newest.tabIndex === -1, 'ArrowLeft must move row focus and tabindex'); rovingBar.children[70].dispatch('keydown', { key: 'Home' }); assert(live.document.activeElement === rovingBar.children[0] && rovingBar.children[0].tabIndex === 0, 'Home must move to the first hour'); rovingBar.children[0].dispatch('keydown', { key: 'End' }); assert(live.document.activeElement === newest && newest.tabIndex === 0, 'End must restore newest hour focus');
  for (const [componentIndex, row] of root.children.entries()) {
    const controls = barFor(row).children;
    assert(['down', 'up', 'no-data'].every(statusClass => controls.some(control => control.className.includes(statusClass))), 'must give down/up/no-data hours distinct semantic color classes');
    assert(controls.every(control => control.tagName === 'BUTTON' && control.attributes['aria-describedby'] === 'status-tooltip' && typeof control.attributes['aria-label'] === 'string' && control.attributes['aria-label'].length > 0 && /正常|故障|无数据/.test(control.attributes['aria-label'])), 'every hourly control must be an accessible button with non-color status text');
    next.components[componentIndex].hours.forEach((hour, hourIndex) => {
      const expectedStatus = hour.status === 'up' ? '正常' : hour.status === 'down' ? '故障' : '无数据';
      const expectedClass = hour.status === 'up' ? 'up' : hour.status === 'down' ? 'down' : 'no-data';
      assert(controls[hourIndex].className.includes(expectedClass) && controls[hourIndex].attributes['aria-label'].includes(expectedStatus), `component ${componentIndex} hour ${hourIndex} must match its own payload status ${expectedStatus}`);
    });
    assert(trackFor(row).children[1].textContent.includes('72 小时前') && trackFor(row).children[1].textContent.includes('现在'), 'each row must keep the 72-hour-to-now axis inside the scroll content');
  }
  assert(root.children[0].children[1].textContent.includes('无数据') && !root.children[0].children[1].textContent.includes('1970'), 'null last_check must be no data');
  let button = barFor(root.children[0]).children[0]; const tooltip = live.document.byId['status-tooltip'];
  button.dispatch('mouseenter'); assert(!tooltip.hidden && tooltip.textContent.includes('检查失败') && !tooltip.textContent.includes('<img'), 'unsafe error must be mapped safely'); assert(!hasImage(tooltip), 'unsafe error must not create an IMG node');
  const upButton = barFor(root.children[0]).children[1]; upButton.dispatch('mouseenter'); assert(tooltip.textContent.includes('状态：正常') && tooltip.textContent.includes('小时可用率：100.00%') && tooltip.textContent.includes('检测：1 / 1') && tooltip.textContent.includes('平均延迟：12 ms'), 'up tooltip must show normal details');
  const noDataButton = barFor(root.children[0]).children[2]; noDataButton.dispatch('mouseenter'); assert(tooltip.textContent.includes('状态：无数据') && tooltip.textContent.includes('小时可用率：无数据') && tooltip.textContent.includes('检测：0 / 0') && tooltip.textContent.includes('平均延迟：无数据'), 'no-data tooltip must show missing-data details');
  live.document.activeElement = rovingBar.children[5]; root.children[0].children[2].scrollLeft = 144; const safePayload = makePayload(); vm.runInContext('render(safePayloadForTest)', Object.assign(live.context, { safePayloadForTest: safePayload })); assert(root.children[0].children[2].scrollLeft === 144 && live.document.activeElement && live.document.activeElement.getAttribute('data-component-key') === 'component-0' && live.document.activeElement.getAttribute('data-hour-index') === '5', 'refresh must restore scroll position and focused hour with preventScroll'); const safeDownButton = barFor(root.children[0]).children[0]; safeDownButton.rect = { left: 900, top: 610, bottom: 628, width: 16, height: 36 }; safeDownButton.dispatch('mouseenter'); const expectedStart = new Date('2025-01-02T03:04:05+08:00').toLocaleString('zh-CN', { hour12: false }); const expectedEnd = new Date('2025-01-02T04:05:06+08:00').toLocaleString('zh-CN', { hour12: false }); const expectedErrorAt = new Date('2025-01-02T03:30:45+08:00').toLocaleString('zh-CN', { hour12: false }); assert(Number.parseFloat(tooltip.style.top) >= 12 && Number.parseFloat(tooltip.style.top) <= 508 && tooltip.textContent.includes('时段：' + expectedStart + ' 至 ' + expectedEnd) && tooltip.textContent.includes('状态：故障') && tooltip.textContent.includes('小时可用率：0.00%') && tooltip.textContent.includes('检测：0 / 1') && tooltip.textContent.includes('平均延迟：12 ms') && tooltip.textContent.includes('最近错误：连接超时') && tooltip.textContent.includes('错误时间：' + expectedErrorAt), 'down tooltip must clamp vertically and show complete mapped failure details with actual timestamps');
  assert(root.children[0].children[2].scrollLeft === 144, 'refresh must preserve each component scroller position');
  button = safeDownButton;
  button.dispatch('mouseleave'); assert(tooltip.hidden && activeIsClear(live.context), 'mouseleave must dismiss tooltip');
  button.dispatch('focus'); assert(!tooltip.hidden, 'focus must show tooltip'); button.dispatch('blur'); assert(tooltip.hidden, 'blur must dismiss tooltip');
  button.dispatch('pointerdown', { pointerType: 'touch', clientX: 10, clientY: 10 }); button.dispatch('pointerup', { pointerType: 'touch', clientX: 10, clientY: 10 }); assert(!tooltip.hidden, 'tap must show tooltip'); button.dispatch('pointerdown', { pointerType: 'touch', clientX: 10, clientY: 10 }); button.dispatch('pointermove', { pointerType: 'touch', clientX: 30, clientY: 10 }); button.dispatch('pointerup', { pointerType: 'touch', clientX: 30, clientY: 10 }); assert(!tooltip.hidden, 'swipe must not toggle tooltip');
  button.dispatch('focus'); live.document.dispatch('keydown', { key: 'Escape' }); assert(tooltip.hidden && activeIsClear(live.context), 'Escape must dismiss tooltip');
  button.focus(); vm.runInContext('render(makePayloadForTest)', Object.assign(live.context, { makePayloadForTest: makePayload() })); assert(live.document.activeElement !== button && live.document.activeElement && live.document.activeElement.getAttribute('data-hour-index') === '0', 'refresh must discard the stale tooltip trigger and restore matching focus');
  const emptyLastCheck = makePayload(); emptyLastCheck.components[0].last_check = ''; vm.runInContext('render(emptyLastCheckForTest)', Object.assign(live.context, { emptyLastCheckForTest: emptyLastCheck })); assert(root.children[0].children[1].textContent.includes('无数据') && !root.children[0].children[1].textContent.includes('1970'), 'empty last_check must be no data');
  const missingLastCheck = makePayload(); delete missingLastCheck.components[0].last_check; vm.runInContext('render(missingLastCheckForTest)', Object.assign(live.context, { missingLastCheckForTest: missingLastCheck })); assert(root.children[0].children[1].textContent.includes('无数据') && !root.children[0].children[1].textContent.includes('1970'), 'missing last_check must be no data');
  const priorFirstRow = root.children[0]; fetchImpl = () => Promise.reject(new Error('offline')); vm.runInContext('loadData()', live.context); await flush(); await flush(); assert(root.children[0] === priorFirstRow && !live.document.byId['refresh-notice'].hidden, 'refresh failure must retain rows and show notice');

  const firstFailure = page(() => Promise.reject(new Error('offline'))); await flush(); await flush(); assert(!firstFailure.document.byId['refresh-notice'].hidden && firstFailure.document.byId['overall-banner'].textContent === '数据暂时无法刷新' && !firstFailure.document.byId['overall-banner'].className.includes('up'), 'first failure must not fake green');

  const slow = page((_url, options) => new Promise((_resolve, reject) => options.signal.addEventListener('abort', () => reject(new Error('aborted'))))); assert(slow.calls() === 1, 'initial load must start once'); vm.runInContext('loadData(); loadData()', slow.context); assert(slow.calls() === 1, 'overlapping loads must issue one fetch'); assert(slow.timers.length > 0, 'request timeout must be scheduled'); slow.timers[0](); await flush(); await flush(); assert(activeIsClear(slow.context) && !slow.document.byId['refresh-notice'].hidden && slow.timers[0] === null, 'timeout must clear timer and show unavailable'); let retryCalls = 0; slow.context.fetch = () => { retryCalls += 1; return Promise.resolve({ ok: true, json: () => Promise.resolve(makePayload()) }); }; vm.runInContext('loadData()', slow.context); await flush(); await flush(); assert(retryCalls === 1, 'timeout must reset inFlight for a retry');
  process.stdout.write('page runtime harness ok\\n');
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
"""


class StatusPageTests(unittest.TestCase):
    def test_page_script_executes_status_rendering_security_and_refresh_contracts(self):
        script_match = re.search(r"<script>\n(.*?)\n</script>", server.HTML_PAGE, re.DOTALL)
        self.assertIsNotNone(script_match)
        result = subprocess.run(
            ["node", "-e", NODE_PAGE_HARNESS],
            input=script_match.group(1),
            text=True,
            capture_output=True,
            check=False,
            timeout=10,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("page runtime harness ok", result.stdout)

    def test_page_keeps_required_markup_and_removes_unsafe_legacy_sinks(self):
        self.assertIn("<title>RSS Pal Status</title>", server.HTML_PAGE)
        self.assertIn("过去 72 小时可用情况", server.HTML_PAGE)
        self.assertIn("setInterval(loadData, 60000)", server.HTML_PAGE)
        self.assertEqual(server.HTML_PAGE.count('role="tooltip"'), 1)
        self.assertIn("hour-scroller", server.HTML_PAGE)
        self.assertNotIn('id="components" class="components" aria-live', server.HTML_PAGE)
        self.assertIn("width: 1941px", server.HTML_PAGE)
        self.assertIn("repeat(72, 24px)", server.HTML_PAGE)
        self.assertIn("max-height: calc(100vh - 24px)", server.HTML_PAGE)
        self.assertIn("timeline-track", server.HTML_PAGE)
        self.assertIn("width: 1941px", server.HTML_PAGE)
        self.assertIn("focus({ preventScroll: true })", server.HTML_PAGE)
        self.assertIn("repeating-linear-gradient", server.HTML_PAGE)
        self.assertIn("dashed", server.HTML_PAGE)
        self.assertNotIn("innerHTML", server.HTML_PAGE)
        self.assertNotIn("insertAdjacentHTML", server.HTML_PAGE)


if __name__ == "__main__":
    unittest.main()
