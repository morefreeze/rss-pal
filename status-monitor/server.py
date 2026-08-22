#!/usr/bin/env python3
"""Collect and serve component status history for RSS Pal."""

import json
import os
import sqlite3
import threading
from concurrent.futures import ThreadPoolExecutor, wait
from http.server import HTTPServer, BaseHTTPRequestHandler
from datetime import datetime, timezone, timedelta
from inspect import signature

import aggregation
from components import Component, ProbeResult, load_components, probe

CST = timezone(timedelta(hours=8))
SELF_COMPONENT = Component("status-monitor", "Status Monitor", "", "self")
SAFE_ERRORS = frozenset(
    {"connection_failed", "connection_timeout", "http_error", "invalid_response"}
)
BUSY_TIMEOUT_MS = 5_000
RETENTION_DAYS = 7
DEFAULT_PROBE_TIMEOUT_SECONDS = signature(probe).parameters["timeout"].default


# --- DB ---
def _connect(db_path):
    conn = sqlite3.connect(db_path, timeout=BUSY_TIMEOUT_MS / 1_000)
    conn.execute(f"PRAGMA busy_timeout = {BUSY_TIMEOUT_MS}")
    return conn


def init_db(db_path):
    """Create the append-only checks schema and non-destructive indexes."""
    directory = os.path.dirname(os.path.abspath(db_path))
    os.makedirs(directory, exist_ok=True)
    conn = _connect(db_path)
    try:
        conn.execute("""
            CREATE TABLE IF NOT EXISTS checks (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                ts TEXT NOT NULL,
                source TEXT NOT NULL DEFAULT 'domain',
                status TEXT NOT NULL,
                code INTEGER,
                latency_ms INTEGER,
                error TEXT
            )
        """)
        conn.execute("CREATE INDEX IF NOT EXISTS idx_checks_ts ON checks(ts)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_checks_source ON checks(source)")
        conn.execute(
            "CREATE INDEX IF NOT EXISTS idx_checks_source_ts ON checks(source, ts)"
        )
        conn.commit()
    finally:
        conn.close()


def _aware_cst(value):
    if value.tzinfo is None or value.utcoffset() is None:
        raise ValueError("now must be timezone-aware")
    return value.astimezone(CST)


def _safe_integer(value):
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    return value


def _safe_result(result):
    if not isinstance(result, ProbeResult):
        return ProbeResult("down", None, None, "connection_failed")
    status = result.status if result.status in {"up", "down"} else "down"
    code = _safe_integer(result.code)
    latency_ms = _safe_integer(result.latency_ms)
    if latency_ms is not None and latency_ms < 0:
        latency_ms = None
    if status == "up":
        error = None
    else:
        error = result.error if result.error in SAFE_ERRORS else "connection_failed"
    return ProbeResult(status, code, latency_ms, error)


class StatusDataUnavailable(Exception):
    """Persisted status rows could not be aggregated safely."""


class MonitorService:
    """Concurrent sampler and aggregated status data source."""

    def __init__(
        self,
        db_path,
        component_defs,
        probe_fn=probe,
        interval_seconds=60,
        now_fn=lambda: datetime.now(CST),
        probe_timeout_seconds=DEFAULT_PROBE_TIMEOUT_SECONDS,
    ):
        real_components = tuple(component_defs)
        if [component.key for component in real_components] != [
            "frontend", "api", "worker", "rsshub", "public"
        ]:
            raise ValueError("component_defs must contain the five real components")
        self.db_path = db_path
        self.real_component_defs = real_components
        self.component_defs = (
            real_components[:4] + (SELF_COMPONENT,) + real_components[4:]
        )
        self.probe_fn = probe_fn
        self.interval_seconds = interval_seconds
        self.now_fn = now_fn
        self.probe_timeout_seconds = float(probe_timeout_seconds)
        if self.probe_timeout_seconds <= 0:
            raise ValueError("probe_timeout_seconds must be positive")
        self._last_cycle_completed_at = None
        self._state_lock = threading.Lock()
        self._cycle_lock = threading.Lock()
        self._executor = ThreadPoolExecutor(max_workers=5)
        self._inflight = {}
        self._closed = False

    @property
    def last_cycle_completed_at(self):
        with self._state_lock:
            return self._last_cycle_completed_at

    def now(self):
        return _aware_cst(self.now_fn())

    @property
    def inflight_count(self):
        with self._cycle_lock:
            return len(self._inflight)

    @property
    def closed(self):
        with self._cycle_lock:
            return self._closed

    def close(self):
        """Cancel queued probes and release the executor without waiting forever."""
        with self._cycle_lock:
            if self._closed:
                return
            self._closed = True
            futures = tuple(self._inflight.values())
            self._inflight.clear()
            for future in futures:
                future.cancel()
            self._executor.shutdown(wait=False, cancel_futures=True)

    def _probe_one(self, component):
        try:
            return _safe_result(self.probe_fn(component))
        except Exception:
            return ProbeResult("down", None, None, "connection_failed")

    def sample_once(self, now):
        """Probe all real components once and atomically persist six rows."""
        cycle_started_at = _aware_cst(now)
        with self._cycle_lock:
            if self._closed:
                raise RuntimeError("monitor service is closed")
            with self._state_lock:
                previous_completion = self._last_cycle_completed_at
            self_up = (
                previous_completion is None
                or cycle_started_at - previous_completion <= timedelta(seconds=120)
            )
            self_result = ProbeResult(
                "up" if self_up else "down",
                None,
                None,
                None if self_up else "connection_failed",
            )

            results = {}
            submitted = {}
            for component in self.real_component_defs:
                prior = self._inflight.get(component.key)
                if prior is not None and prior.done():
                    self._inflight.pop(component.key, None)
                    prior = None
                if prior is not None:
                    results[component.key] = ProbeResult(
                        "down", None, None, "connection_timeout"
                    )
                    continue
                future = self._executor.submit(self._probe_one, component)
                self._inflight[component.key] = future
                submitted[component.key] = future

            completed, _unfinished = wait(
                tuple(submitted.values()), timeout=self.probe_timeout_seconds
            )
            for key, future in submitted.items():
                if future in completed:
                    results[key] = future.result()
                    if self._inflight.get(key) is future:
                        self._inflight.pop(key, None)
                else:
                    results[key] = ProbeResult(
                        "down", None, None, "connection_timeout"
                    )
            results[SELF_COMPONENT.key] = self_result

            timestamp = cycle_started_at.isoformat()
            rows = [
                (
                    timestamp,
                    component.key,
                    results[component.key].status,
                    results[component.key].code,
                    results[component.key].latency_ms,
                    results[component.key].error,
                )
                for component in self.component_defs
            ]
            cutoff = (cycle_started_at - timedelta(days=RETENTION_DAYS)).isoformat()
            conn = _connect(self.db_path)
            try:
                conn.executemany(
                    "INSERT INTO checks "
                    "(ts, source, status, code, latency_ms, error) "
                    "VALUES (?, ?, ?, ?, ?, ?)",
                    rows,
                )
                conn.execute("DELETE FROM checks WHERE ts < ?", (cutoff,))
                conn.commit()
            finally:
                conn.close()

            cycle_completed_at = _aware_cst(self.now_fn())
            with self._state_lock:
                self._last_cycle_completed_at = cycle_completed_at

    def payload(self, now):
        """Read the database and return the public aggregate schema."""
        payload_now = _aware_cst(now)
        conn = _connect(self.db_path)
        try:
            conn.execute("BEGIN")
            try:
                try:
                    data = aggregation.status_payload(
                        conn,
                        self.component_defs,
                        payload_now,
                        self.interval_seconds,
                    )
                except (ValueError, TypeError):
                    raise StatusDataUnavailable from None
                conn.commit()
                return data
            except Exception:
                if conn.in_transaction:
                    conn.rollback()
                raise
        finally:
            conn.close()


def sampling_loop(service, stop_event=None):
    """Sample immediately, then wait one configured interval between cycles."""
    stopper = stop_event or threading.Event()
    while not stopper.is_set():
        try:
            service.sample_once(service.now())
        except Exception:
            pass
        stopper.wait(service.interval_seconds)


# --- HTTP ---
HTML_PAGE = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>RSS Pal - 站点状态</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    background: #0d1117; color: #c9d1d9; min-height: 100vh;
    padding: 2rem 1rem;
  }
  .container { max-width: 720px; margin: 0 auto; }
  h1 { font-size: 1.5rem; margin-bottom: 0.5rem; color: #f0f6fc; }
  .domain { font-size: 0.85rem; color: #8b949e; margin-bottom: 1.5rem; font-family: monospace; }

  .section { margin-bottom: 2rem; }
  .section-title {
    font-size: 1rem; color: #f0f6fc; margin-bottom: 0.75rem;
    display: flex; align-items: center; gap: 0.5rem;
  }
  .section-title .badge {
    font-size: 0.65rem; padding: 2px 8px; border-radius: 10px;
    background: #1f6feb33; color: #58a6ff; font-weight: 600;
  }

  .stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 1rem; margin-bottom: 1rem; }
  .stat-card {
    background: #161b22; border: 1px solid #30363d; border-radius: 8px;
    padding: 1rem; text-align: center;
  }
  .stat-card .label { font-size: 0.75rem; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }
  .stat-card .value { font-size: 1.8rem; font-weight: 700; margin-top: 0.25rem; }
  .stat-card .detail { font-size: 0.7rem; color: #484f58; margin-top: 0.25rem; }
  .value.green { color: #3fb950; }
  .value.red { color: #f85149; }
  .value.blue { color: #58a6ff; }
  .value.orange { color: #d29922; }

  .bar-container {
    display: flex; gap: 2px; height: 32px; border-radius: 4px; overflow: hidden;
    background: #161b22; border: 1px solid #30363d; padding: 4px;
  }
  .bar-segment { flex: 1; min-width: 2px; border-radius: 2px; transition: opacity 0.2s; cursor: pointer; position: relative; }
  .bar-segment.up { background: #3fb950; }
  .bar-segment.down { background: #f85149; }
  .bar-segment:hover { opacity: 0.7; }
  .bar-segment[title]:hover::after {
    content: attr(title); position: absolute; bottom: 110%; left: 50%; transform: translateX(-50%);
    background: #1c2128; border: 1px solid #30363d; padding: 4px 8px; border-radius: 4px;
    font-size: 0.7rem; white-space: nowrap; z-index: 10; color: #c9d1d9;
  }
  .legend { display: flex; gap: 1rem; margin-top: 0.5rem; font-size: 0.75rem; color: #8b949e; }
  .legend span::before { content: ''; display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 4px; vertical-align: middle; }
  .legend .up-legend::before { background: #3fb950; }
  .legend .down-legend::before { background: #f85149; }

  .incidents { margin-top: 1rem; }
  .incidents h2 { font-size: 1rem; margin-bottom: 0.75rem; color: #f0f6fc; }
  .incident {
    background: #161b22; border: 1px solid #30363d; border-radius: 6px;
    padding: 0.75rem 1rem; margin-bottom: 0.5rem; font-size: 0.85rem;
  }
  .incident .time { color: #8b949e; font-family: monospace; font-size: 0.75rem; }
  .incident .source-tag { font-size: 0.65rem; padding: 1px 6px; border-radius: 8px; margin-left: 0.5rem; }
  .incident .source-tag.local { background: #3fb95022; color: #3fb950; }
  .incident .source-tag.domain { background: #58a6ff22; color: #58a6ff; }
  .incident .error { color: #f85149; margin-top: 0.25rem; word-break: break-all; }
  .no-incidents { color: #8b949e; font-size: 0.85rem; }

  .footer { text-align: center; margin-top: 2rem; font-size: 0.75rem; color: #484f58; }
</style>
</head>
<body>
<div class="container">
  <h1>📡 RSS Pal 站点状态</h1>
  <div class="domain" id="domain"></div>

  <!-- Local section -->
  <div class="section" id="local-section">
    <div class="section-title">🏠 本地服务 <span class="badge">Docker 内网</span></div>
    <div class="stats">
      <div class="stat-card">
        <div class="label">可用率</div>
        <div class="value green" id="local-uptime">—</div>
      </div>
      <div class="stat-card">
        <div class="label">平均延迟</div>
        <div class="value blue" id="local-latency">—</div>
      </div>
      <div class="stat-card">
        <div class="label">最近状态</div>
        <div class="value" id="local-status">—</div>
      </div>
    </div>
    <div class="bar-container" id="local-timeline"></div>
    <div class="legend">
      <span class="up-legend">正常</span>
      <span class="down-legend">故障</span>
    </div>
  </div>

  <!-- Domain section -->
  <div class="section" id="domain-section">
    <div class="section-title">🌐 外部访问 <span class="badge" id="domain-badge">Tailscale Funnel</span></div>
    <div class="stats">
      <div class="stat-card">
        <div class="label">可用率</div>
        <div class="value green" id="domain-uptime">—</div>
      </div>
      <div class="stat-card">
        <div class="label">平均延迟</div>
        <div class="value blue" id="domain-latency">—</div>
        <div class="detail" id="domain-latency-detail"></div>
      </div>
      <div class="stat-card">
        <div class="label">最近状态</div>
        <div class="value" id="domain-status">—</div>
      </div>
    </div>
    <div class="bar-container" id="domain-timeline"></div>
    <div class="legend">
      <span class="up-legend">正常</span>
      <span class="down-legend">故障</span>
    </div>
  </div>

  <div class="incidents">
    <h2>最近故障记录</h2>
    <div id="incidents-list"></div>
  </div>

  <div class="footer">
    自动刷新 · 每 <span id="interval"></span> 秒检测一次
  </div>
</div>

<script>
const HOUR = 3600000;
const HOURS = 72;

function renderStats(prefix, data) {
  const uptimeEl = document.getElementById(prefix + '-uptime');
  uptimeEl.textContent = data.uptime_pct + '%';
  uptimeEl.className = 'value ' + (data.uptime_pct >= 99 ? 'green' : data.uptime_pct >= 95 ? 'blue' : 'red');

  const latencyEl = document.getElementById(prefix + '-latency');
  latencyEl.textContent = data.avg_latency_ms + 'ms';
  if (prefix === 'local') {
    latencyEl.className = 'value ' + (data.avg_latency_ms <= 50 ? 'green' : data.avg_latency_ms <= 200 ? 'blue' : 'orange');
  } else {
    latencyEl.className = 'value ' + (data.avg_latency_ms <= 500 ? 'green' : data.avg_latency_ms <= 1000 ? 'orange' : 'red');
  }

  const lastEl = document.getElementById(prefix + '-status');
  if (data.checks.length > 0) {
    const last = data.checks[0];
    lastEl.textContent = last.status === 'up' ? '✅ UP' : '❌ DOWN';
    lastEl.className = 'value ' + (last.status === 'up' ? 'green' : 'red');
  }

  // Detail for domain latency
  if (prefix === 'domain' && data.checks.length > 0) {
    const lats = data.checks.filter(c => c.latency_ms != null).map(c => c.latency_ms);
    if (lats.length > 0) {
      const mn = Math.min(...lats), mx = Math.max(...lats);
      document.getElementById('domain-latency-detail').textContent = mn + 'ms ~ ' + mx + 'ms';
    }
  }
}

function renderTimeline(barId, checks) {
  const bar = document.getElementById(barId);
  bar.innerHTML = '';
  const now = Date.now();
  const buckets = new Array(HOURS).fill(null).map(() => ({up: 0, down: 0}));

  checks.forEach(c => {
    const t = new Date(c.ts).getTime();
    const hoursAgo = Math.floor((now - t) / HOUR);
    if (hoursAgo >= 0 && hoursAgo < HOURS) {
      const idx = HOURS - 1 - hoursAgo;
      if (c.status === 'up') buckets[idx].up++;
      else buckets[idx].down++;
    }
  });

  buckets.forEach((b, i) => {
    const div = document.createElement('div');
    div.className = 'bar-segment';
    const total = b.up + b.down;
    if (total === 0) {
      div.style.background = '#21262d';
    } else if (b.down === 0) {
      div.classList.add('up');
    } else if (b.up === 0) {
      div.classList.add('down');
    } else {
      div.style.background = 'linear-gradient(to top, #f85149 ' + Math.round(b.down/total*100) + '%, #3fb950 ' + Math.round(b.down/total*100) + '%)';
    }
    const hourLabel = new Date(now - (HOURS - 1 - i) * HOUR);
    const hh = hourLabel.getHours().toString().padStart(2, '0');
    div.title = hh + ':00 · ' + (total > 0 ? b.up + '/' + total + ' OK' : '无数据');
    bar.appendChild(div);
  });
}

function render(data) {
  document.getElementById('domain').textContent = data.domain;
  document.getElementById('interval').textContent = data.check_interval || 60;

  if (data.local) {
    renderStats('local', data.local);
    renderTimeline('local-timeline', data.local.checks);
  }
  if (data.domain_stats) {
    renderStats('domain', data.domain_stats);
    renderTimeline('domain-timeline', data.domain_stats.checks);
  }

  // Combined incidents
  const allChecks = [];
  if (data.domain_stats) {
    data.domain_stats.checks.forEach(c => allChecks.push({...c, source: 'domain'}));
  }
  if (data.local) {
    data.local.checks.forEach(c => allChecks.push({...c, source: 'local'}));
  }
  allChecks.sort((a, b) => b.ts.localeCompare(a.ts));
  const incidents = allChecks.filter(c => c.status === 'down').slice(0, 20);
  const list = document.getElementById('incidents-list');
  if (incidents.length === 0) {
    list.innerHTML = '<div class="no-incidents">🎉 暂无故障记录</div>';
  } else {
    list.innerHTML = incidents.map(c =>
      '<div class="incident"><div class="time">' + c.ts +
      '<span class="source-tag ' + c.source + '">' + (c.source === 'local' ? '本地' : '外部') + '</span></div>' +
      '<div class="error">' + (c.error || '连接失败') + '</div></div>'
    ).join('');
  }
}

function loadData() {
  fetch('/api/status')
    .then(r => r.json())
    .then(render)
    .catch(() => {});
}

loadData();
setInterval(loadData, 30000);
</script>
</body>
</html>"""


def make_handler(service, html_page=HTML_PAGE):
    """Build an HTTP handler bound to one monitor service instance."""

    class Handler(BaseHTTPRequestHandler):
        def _send(self, status, content_type, body):
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Access-Control-Allow-Origin", "*")
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/api/status":
                request_now = service.now()
                try:
                    data = service.payload(request_now)
                except (sqlite3.Error, StatusDataUnavailable):
                    body = json.dumps(
                        {"error": "status_data_unavailable"}, separators=(",", ":")
                    ).encode("utf-8")
                    self._send(500, "application/json; charset=utf-8", body)
                    return
                body = json.dumps(data, ensure_ascii=False).encode("utf-8")
                self._send(200, "application/json; charset=utf-8", body)
                return

            body = html_page.encode("utf-8")
            self._send(200, "text/html; charset=utf-8", body)

        def log_message(self, _format, *_args):
            pass

    return Handler


def main(env=None):
    runtime_env = os.environ if env is None else env
    db_path = runtime_env.get("DB_PATH", "/data/status.db")
    interval_seconds = int(runtime_env.get("CHECK_INTERVAL", "60"))
    port = int(runtime_env.get("MONITOR_PORT", "8090"))
    component_defs = load_components(runtime_env)

    init_db(db_path)
    service = MonitorService(
        db_path=db_path,
        component_defs=component_defs,
        probe_fn=probe,
        interval_seconds=interval_seconds,
        now_fn=lambda: datetime.now(CST),
    )
    stop_event = threading.Event()
    httpd = None
    sampler = None
    sampler_started = False
    try:
        httpd = HTTPServer(("0.0.0.0", port), make_handler(service))
        sampler = threading.Thread(
            target=sampling_loop, args=(service, stop_event), daemon=True
        )
        sampler.start()
        sampler_started = True
        httpd.serve_forever()
    finally:
        stop_event.set()
        try:
            if httpd is not None:
                httpd.server_close()
        finally:
            try:
                service.close()
            finally:
                if sampler_started:
                    sampler.join(timeout=1)


if __name__ == "__main__":
    main()
