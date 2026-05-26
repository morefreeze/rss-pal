#!/usr/bin/env python3
"""Dual-mode uptime monitor: local service health + external domain latency."""

import json
import os
import sqlite3
import ssl
import threading
import time
import http.client
import urllib.request
import urllib.error
from http.server import HTTPServer, BaseHTTPRequestHandler
from datetime import datetime, timezone, timedelta

# --- Config ---
DOMAIN = os.getenv("MONITOR_DOMAIN", "https://freezemacbook-pro.tailf22f5.ts.net")
LOCAL_URL = os.getenv("LOCAL_URL", "http://frontend:80")
CHECK_INTERVAL = int(os.getenv("CHECK_INTERVAL", "60"))
PORT = int(os.getenv("MONITOR_PORT", "8090"))
DB_PATH = os.getenv("DB_PATH", "/data/status.db")
HISTORY_HOURS = int(os.getenv("HISTORY_HOURS", "72"))

CST = timezone(timedelta(hours=8))


# --- DB ---
def init_db():
    os.makedirs(os.path.dirname(DB_PATH), exist_ok=True)
    conn = sqlite3.connect(DB_PATH)
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
    conn.commit()
    conn.close()


def record_check(source, status, code=None, latency_ms=None, error=None):
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        "INSERT INTO checks (ts, source, status, code, latency_ms, error) VALUES (?, ?, ?, ?, ?, ?)",
        (datetime.now(CST).isoformat(), source, status, code, latency_ms, error)
    )
    cutoff = (datetime.now(CST) - timedelta(days=7)).isoformat()
    conn.execute("DELETE FROM checks WHERE ts < ?", (cutoff,))
    conn.commit()
    conn.close()


def get_stats_for(source, conn):
    cutoff = (datetime.now(CST) - timedelta(hours=HISTORY_HOURS)).isoformat()
    rows = conn.execute(
        "SELECT ts, status, code, latency_ms, error FROM checks WHERE source = ? AND ts > ? ORDER BY ts DESC",
        (source, cutoff)
    ).fetchall()

    total = len(rows)
    if total == 0:
        return {"total": 0, "up": 0, "down": 0, "uptime_pct": 0, "avg_latency_ms": 0, "checks": []}

    up = sum(1 for r in rows if r[1] == "up")
    down = total - up
    latencies = [r[3] for r in rows if r[3] is not None]
    avg_lat = sum(latencies) / len(latencies) if latencies else 0

    checks = []
    for r in rows:
        checks.append({
            "ts": r[0], "status": r[1], "code": r[2],
            "latency_ms": r[3], "error": r[4]
        })

    return {
        "total": total, "up": up, "down": down,
        "uptime_pct": round(up / total * 100, 2) if total else 0,
        "avg_latency_ms": round(avg_lat),
        "last_check": rows[0][0] if rows else None,
        "checks": checks,
    }


def get_stats():
    conn = sqlite3.connect(DB_PATH)
    local = get_stats_for("local", conn)
    conn.close()

    # Read domain checks from host-mounted DB (written by host cron job)
    domain_stats = {"total": 0, "up": 0, "down": 0, "uptime_pct": 0, "avg_latency_ms": 0, "checks": []}
    domain_db = os.path.join(os.path.dirname(DB_PATH), "domain.db")
    if os.path.exists(domain_db):
        try:
            dconn = sqlite3.connect(domain_db)
            domain_stats = get_stats_for("domain", dconn)
            dconn.close()
        except Exception:
            pass

    return {
        "domain": DOMAIN,
        "check_interval": CHECK_INTERVAL,
        "local": local,
        "domain_stats": domain_stats,
    }


# --- Domain checker with connection reuse ---
class DomainChecker:
    """Reuses HTTPS connection to avoid TLS handshake per check."""
    def __init__(self, domain_url):
        from urllib.parse import urlparse
        parsed = urlparse(domain_url)
        self.host = parsed.hostname
        self.port = parsed.port or 443
        self.path = parsed.path or "/"
        self._conn = None
        self._lock = threading.Lock()

    def _new_conn(self):
        ctx = ssl.create_default_context()
        return http.client.HTTPSConnection(self.host, self.port, context=ctx, timeout=10)

    def check(self):
        with self._lock:
            start = time.monotonic()
            try:
                if self._conn is None:
                    self._conn = self._new_conn()
                self._conn.request("GET", self.path, headers={"User-Agent": "rss-pal-monitor/1.0", "Connection": "keep-alive"})
                resp = self._conn.getresponse()
                latency = int((time.monotonic() - start) * 1000)
                # Drain body so connection can be reused
                resp.read()
                return "up", resp.status, latency, None
            except Exception as e:
                # Connection dead, will recreate next time
                self._conn = None
                if 'start' in dir():
                    latency = int((time.monotonic() - start) * 1000)
                else:
                    latency = None
                return "down", None, latency, str(e)[:200]

domain_checker = DomainChecker(DOMAIN)


# --- Checker ---
def do_check(url, source):
    if source == "domain":
        status, code, latency, error = domain_checker.check()
        record_check(source, status, code=code, latency_ms=latency, error=error)
        return
    try:
        start = time.monotonic()
        req = urllib.request.Request(url, method="GET", headers={"User-Agent": "rss-pal-monitor/1.0"})
        resp = urllib.request.urlopen(req, timeout=10)
        latency = int((time.monotonic() - start) * 1000)
        record_check(source, "up", code=resp.status, latency_ms=latency)
    except urllib.error.HTTPError as e:
        latency = int((time.monotonic() - start) * 1000)
        record_check(source, "up", code=e.code, latency_ms=latency)
    except Exception as e:
        record_check(source, "down", error=str(e)[:200])


def checker_loop():
    while True:
        try:
            do_check(LOCAL_URL, "local")
        except Exception:
            pass
        time.sleep(CHECK_INTERVAL)


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


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/api/status':
            data = get_stats()
            body = json.dumps(data, ensure_ascii=False).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.end_headers()
            self.wfile.write(body)
        else:
            body = HTML_PAGE.encode()
            self.send_response(200)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.end_headers()
            self.wfile.write(body)

    def log_message(self, format, *args):
        pass


if __name__ == '__main__':
    init_db()
    t = threading.Thread(target=checker_loop, daemon=True)
    t.start()
    print(f"Monitor running: local={LOCAL_URL} domain={DOMAIN} every {CHECK_INTERVAL}s")
    print(f"Status page: http://0.0.0.0:{PORT}")
    server = HTTPServer(('0.0.0.0', PORT), Handler)
    server.serve_forever()
