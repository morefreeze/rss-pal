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
<title>RSS Pal Status</title>
<style>
  :root { color-scheme: light; --ink: #20242b; --muted: #5f6875; --line: #d9dee5; --panel: #ffffff; --page: #f5f7f9; --up: #18794e; --down: #c9372c; --none: #73808c; }
  * { box-sizing: border-box; }
  body { margin: 0; min-width: 280px; background: var(--page); color: var(--ink); font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
  .page { max-width: 1040px; margin: 0 auto; padding: 32px 20px 48px; }
  .header { display: flex; justify-content: space-between; gap: 16px; align-items: start; margin-bottom: 20px; }
  h1 { margin: 0 0 6px; font-size: clamp(1.5rem, 4vw, 2rem); letter-spacing: -0.02em; }
  .updated, .refresh { margin: 0; color: var(--muted); font-size: .875rem; }
  .refresh { white-space: nowrap; padding-top: 6px; }
  .banner { border: 1px solid; border-radius: 8px; padding: 14px 16px; font-weight: 650; margin-bottom: 28px; }
  .banner.loading { border-color: #aab4bf; background: #eef1f4; color: #3e4a57; }
  .banner.up { border-color: #9bd1b4; background: #edf9f1; color: #0d5a36; }
  .banner.down { border-color: #efb2ad; background: #fff0ef; color: #9b251d; }
  .notice { margin: -14px 0 16px; color: #9b251d; font-size: .875rem; }
  .notice[hidden], .tooltip[hidden] { display: none; }
  h2 { margin: 0 0 4px; font-size: 1.125rem; }
  .caption { margin: 0 0 18px; color: var(--muted); font-size: .875rem; }
  .components { background: var(--panel); border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
  .component { padding: 16px; border-bottom: 1px solid var(--line); }
  .component:last-child { border-bottom: 0; }
  .component-meta { display: grid; grid-template-columns: minmax(160px, 1fr) auto auto; gap: 12px; align-items: center; margin-bottom: 10px; }
  .component-name { font-weight: 650; min-width: 0; overflow-wrap: anywhere; }
  .current-status { font-size: .8125rem; font-weight: 650; border-radius: 999px; padding: 3px 8px; }
  .current-status.up { color: #0d5a36; background: #dff5e7; }
  .current-status.down { color: #9b251d; background: #ffdfdc; }
  .uptime { color: var(--muted); font-size: .8125rem; text-align: right; white-space: nowrap; }
  .last-check { color: var(--muted); font-size: .75rem; margin: -4px 0 9px; }
  .hour-scroller { overflow-x: auto; overscroll-behavior-x: contain; -webkit-overflow-scrolling: touch; }
  .timeline-track { width: 100%; min-width: 100%; }
  .hour-bar { display: grid; width: 100%; grid-template-columns: repeat(72, minmax(0, 1fr)); gap: 2px; height: 26px; }
  .hour-control { appearance: none; border: 0; border-radius: 2px; min-width: 0; padding: 0; cursor: pointer; }
  .hour-control.up { background: var(--up); }
  .hour-control.down { background: repeating-linear-gradient(135deg, var(--down), var(--down) 3px, #a8211a 3px, #a8211a 5px); border: 1px solid #7e1914; }
  .hour-control.no-data { background: var(--none); border: 1px dashed #35404a; }
  .hour-control:hover, .hour-control:focus-visible { outline: 2px solid #1f6feb; outline-offset: 2px; opacity: .82; }
  .axis { display: flex; justify-content: space-between; color: var(--muted); font-size: .75rem; margin-top: 6px; }
  .legend { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 18px; color: var(--muted); font-size: .8125rem; }
  .legend-item::before { content: ""; display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 5px; }
  .legend-item.up::before { background: var(--up); }
  .legend-item.down::before { background: repeating-linear-gradient(135deg, var(--down), var(--down) 3px, #a8211a 3px, #a8211a 5px); border: 1px solid #7e1914; }
  .legend-item.no-data::before { background: var(--none); border: 1px dashed #35404a; }
  .tooltip { position: fixed; z-index: 10; max-width: min(320px, calc(100vw - 24px)); max-height: calc(100vh - 24px); overflow-y: auto; padding: 10px 12px; border: 1px solid #454d57; border-radius: 6px; background: #20242b; color: #fff; box-shadow: 0 6px 20px rgba(0,0,0,.2); font-size: .8125rem; line-height: 1.45; pointer-events: none; }
  .tooltip-title { font-weight: 650; margin-bottom: 4px; }
  .tooltip-line { overflow-wrap: anywhere; }
  @media (max-width: 650px) {
    .page { padding: 22px 12px 32px; }
    .header { display: block; }
    .refresh { padding-top: 8px; }
    .component { padding: 13px 10px; }
    .component-meta { grid-template-columns: minmax(0, 1fr) auto auto; gap: 7px; }
    .current-status, .uptime { font-size: .75rem; }
    .hour-scroller { margin-right: -10px; padding: 2px 10px 6px 0; }
    .timeline-track { width: 1941px; min-width: 1941px; }
    .hour-bar, .axis { width: 100%; min-width: 100%; }
    .hour-bar { grid-template-columns: repeat(72, 24px); gap: 3px; height: 40px; }
    .hour-control { width: 24px; height: 40px; }
  }
</style>
</head>
<body>
<main class="page">
  <header class="header">
    <div>
      <h1>RSS Pal Status</h1>
      <p class="updated">最后更新：<span id="last-update">—</span></p>
    </div>
    <p id="refresh-label" class="refresh">每 60 秒刷新</p>
  </header>
  <section id="overall-banner" class="banner loading" aria-live="polite">正在加载状态…</section>
  <p id="refresh-notice" class="notice" role="status" hidden>数据暂时无法刷新</p>
  <section aria-labelledby="history-title">
    <h2 id="history-title">过去 72 小时可用情况</h2>
    <p class="caption">每个方块代表一个自然小时；可聚焦或点按方块查看检测详情。</p>
    <div id="components" class="components"></div>
    <div class="legend" aria-label="状态图例">
      <span class="legend-item up">绿色：正常</span>
      <span class="legend-item down">红色：故障</span>
      <span class="legend-item no-data">灰色：无数据</span>
    </div>
  </section>
</main>
<div id="status-tooltip" class="tooltip" role="tooltip" hidden></div>
<script>
const HOURS = 72;
const REQUEST_TIMEOUT_MS = 10000;
const ERROR_LABELS = Object.freeze({
  connection_failed: '连接失败',
  connection_timeout: '连接超时',
  http_error: 'HTTP 检查失败',
  invalid_response: '响应无效'
});
let inFlight = false;
let lastSuccessfulData = null;
let activeTooltipButton = null;

const tooltip = document.getElementById('status-tooltip');
const componentsRoot = document.getElementById('components');
const overallBanner = document.getElementById('overall-banner');
const refreshNotice = document.getElementById('refresh-notice');
const refreshLabel = document.getElementById('refresh-label');

function setText(element, value) {
  element.textContent = value == null ? '—' : String(value);
}

function safeErrorLabel(value) {
  return typeof value === 'string' && Object.prototype.hasOwnProperty.call(ERROR_LABELS, value)
    ? ERROR_LABELS[value]
    : '检查失败';
}

function hourStatusText(value) {
  if (value === 'up') return '正常';
  if (value === 'down') return '故障';
  return '无数据';
}

function formatTimestamp(value) {
  if (value == null || (typeof value === 'string' && value.trim() === '')) return '无数据';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '无数据';
  return date.toLocaleString('zh-CN', { hour12: false });
}

function formatPercent(value) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(2) + '%' : '无数据';
}

function formatCount(value) {
  return Number.isInteger(value) && value >= 0 ? String(value) : '0';
}

function appendTooltipLine(label, value) {
  const line = document.createElement('div');
  line.className = 'tooltip-line';
  setText(line, label + value);
  tooltip.appendChild(line);
}

function hourRange(hour) {
  return formatTimestamp(hour.start) + ' 至 ' + formatTimestamp(hour.end);
}

function tooltipLabel(component, hour) {
  return component.name + '，' + hourRange(hour) + '，' + hourStatusText(hour.status)
    + '，小时可用率 ' + formatPercent(hour.uptime_pct)
    + '，检测 ' + formatCount(hour.successful_checks) + ' / ' + formatCount(hour.total_checks);
}

function showTooltip(button, component, hour) {
  tooltip.replaceChildren();
  const title = document.createElement('div');
  title.className = 'tooltip-title';
  setText(title, component.name);
  tooltip.appendChild(title);
  appendTooltipLine('时段：', hourRange(hour));
  appendTooltipLine('状态：', hourStatusText(hour.status));
  appendTooltipLine('小时可用率：', formatPercent(hour.uptime_pct));
  appendTooltipLine('检测：', formatCount(hour.successful_checks) + ' / ' + formatCount(hour.total_checks));
  appendTooltipLine('平均延迟：', hour.avg_latency_ms == null ? '无数据' : formatCount(hour.avg_latency_ms) + ' ms');
  if (hour.status === 'down') {
    appendTooltipLine('最近错误：', safeErrorLabel(hour.last_error));
    appendTooltipLine('错误时间：', formatTimestamp(hour.last_error_at));
  }
  tooltip.hidden = false;
  const bounds = button.getBoundingClientRect();
  const tipBounds = tooltip.getBoundingClientRect();
  const margin = 12;
  const tipWidth = tipBounds.width || 320;
  const tipHeight = tipBounds.height || 120;
  const triggerBottom = bounds.bottom == null ? bounds.top : bounds.bottom;
  const left = Math.max(margin, Math.min(bounds.left, window.innerWidth - tipWidth - margin));
  let top = triggerBottom + 8;
  if (top + tipHeight > window.innerHeight - margin) top = bounds.top - tipHeight - 8;
  tooltip.style.left = Math.max(margin, Math.min(left, window.innerWidth - tipWidth - margin)) + 'px';
  tooltip.style.top = Math.max(margin, Math.min(top, window.innerHeight - tipHeight - margin)) + 'px';
  activeTooltipButton = button;
}

function dismissTooltip() {
  tooltip.hidden = true;
  activeTooltipButton = null;
}

function setRovingButton(button) {
  const buttons = button.parentNode.children;
  for (const candidate of buttons) candidate.tabIndex = candidate === button ? 0 : -1;
}

function moveRovingButton(button, key) {
  const buttons = button.parentNode.children;
  const currentIndex = Array.prototype.indexOf.call(buttons, button);
  let nextIndex = currentIndex;
  if (key === 'ArrowLeft') nextIndex = Math.max(0, currentIndex - 1);
  if (key === 'ArrowRight') nextIndex = Math.min(buttons.length - 1, currentIndex + 1);
  if (key === 'Home') nextIndex = 0;
  if (key === 'End') nextIndex = buttons.length - 1;
  if (nextIndex !== currentIndex || key === 'Home' || key === 'End') {
    const next = buttons[nextIndex];
    setRovingButton(next);
    next.focus();
  }
}

function toggleTooltip(button, component, hour) {
  if (activeTooltipButton === button && !tooltip.hidden) dismissTooltip();
  else showTooltip(button, component, hour);
}

function createHourButton(component, hour, hourIndex) {
  const button = document.createElement('button');
  const statusClass = hour.status === 'up' ? 'up' : hour.status === 'down' ? 'down' : 'no-data';
  button.type = 'button';
  button.className = 'hour-control ' + statusClass;
  button.tabIndex = hourIndex === HOURS - 1 ? 0 : -1;
  button.setAttribute('data-component-key', component.key);
  button.setAttribute('data-hour-index', String(hourIndex));
  button.setAttribute('aria-describedby', 'status-tooltip');
  button.setAttribute('aria-label', tooltipLabel(component, hour));
  button.addEventListener('mouseenter', () => showTooltip(button, component, hour));
  button.addEventListener('mouseleave', dismissTooltip);
  button.addEventListener('focus', () => { setRovingButton(button); if (!button._pointerGesture) showTooltip(button, component, hour); });
  button.addEventListener('blur', dismissTooltip);
  button.addEventListener('pointerdown', event => { button._pointerGesture = { x: event.clientX, y: event.clientY, dragged: false }; setRovingButton(button); });
  button.addEventListener('pointermove', event => { const gesture = button._pointerGesture; if (gesture && Math.hypot(event.clientX - gesture.x, event.clientY - gesture.y) > 10) gesture.dragged = true; });
  button.addEventListener('pointerup', () => { const gesture = button._pointerGesture; button._pointerGesture = null; if (gesture && !gesture.dragged) toggleTooltip(button, component, hour); });
  button.addEventListener('pointercancel', () => { button._pointerGesture = null; });
  button.addEventListener('keydown', event => {
    if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) {
      event.preventDefault();
      moveRovingButton(button, event.key);
    }
    if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); toggleTooltip(button, component, hour); }
  });
  return button;
}

function validatePayload(data) {
  if (!data || !Array.isArray(data.components) || data.components.length !== 6) {
    throw new Error('invalid component status payload');
  }
  for (const component of data.components) {
    if (!component || typeof component.key !== 'string'
      || !Array.isArray(component.hours) || component.hours.length !== HOURS) {
      throw new Error('invalid component history');
    }
  }
}

function renderComponent(component) {
  const row = document.createElement('article');
  row.className = 'component';
  row.setAttribute('data-component-key', component.key);
  const meta = document.createElement('div');
  meta.className = 'component-meta';
  const name = document.createElement('div');
  name.className = 'component-name';
  setText(name, component.name);
  const status = document.createElement('span');
  const isUp = component.current_status === 'up';
  status.className = 'current-status ' + (isUp ? 'up' : 'down');
  setText(status, isUp ? '正常' : '故障');
  const uptime = document.createElement('div');
  uptime.className = 'uptime';
  setText(uptime, '可用率 ' + formatPercent(component.uptime_pct));
  meta.append(name, status, uptime);
  row.appendChild(meta);
  const lastCheck = document.createElement('div');
  lastCheck.className = 'last-check';
  setText(lastCheck, '最近检查：' + formatTimestamp(component.last_check));
  row.appendChild(lastCheck);
  const scroller = document.createElement('div');
  scroller.className = 'hour-scroller';
  scroller.setAttribute('aria-label', component.name + ' 的 72 小时状态，可横向滑动');
  const bar = document.createElement('div');
  bar.className = 'hour-bar';
  component.hours.forEach((hour, hourIndex) => bar.appendChild(createHourButton(component, hour, hourIndex)));
  const track = document.createElement('div');
  track.className = 'timeline-track';
  track.appendChild(bar);
  const axis = document.createElement('div');
  axis.className = 'axis';
  const ago = document.createElement('span');
  const now = document.createElement('span');
  setText(ago, '72 小时前');
  setText(now, '现在');
  axis.append(ago, now);
  track.appendChild(axis);
  scroller.appendChild(track);
  row.appendChild(scroller);
  return row;
}

function render(data) {
  validatePayload(data);
  const active = document.activeElement;
  const scrollPositions = new Map(Array.from(componentsRoot.children).map(row => [row.getAttribute('data-component-key'), row.children[2].scrollLeft]));
  const focusedHour = active && active.className && active.className.includes('hour-control')
    ? { key: active.getAttribute('data-component-key'), index: Number(active.getAttribute('data-hour-index')) }
    : null;
  const fragment = document.createDocumentFragment();
  data.components.forEach(component => fragment.appendChild(renderComponent(component)));
  dismissTooltip();
  componentsRoot.replaceChildren(fragment);
  for (const row of componentsRoot.children) row.children[2].scrollLeft = scrollPositions.get(row.getAttribute('data-component-key')) || 0;
  if (focusedHour && Number.isInteger(focusedHour.index)) {
    const row = Array.from(componentsRoot.children).find(item => item.getAttribute('data-component-key') === focusedHour.key);
    const scroller = row && row.children[2];
    const restored = scroller && scroller.children[0].children[0].children[focusedHour.index];
    if (restored) {
      const savedScrollLeft = scroller.scrollLeft;
      setRovingButton(restored);
      try { restored.focus({ preventScroll: true }); } catch (_error) { restored.focus(); }
      scroller.scrollLeft = savedScrollLeft;
      if (typeof requestAnimationFrame === 'function') requestAnimationFrame(() => { scroller.scrollLeft = savedScrollLeft; });
    }
  }
  setText(document.getElementById('last-update'), formatTimestamp(data.generated_at));
  setText(refreshLabel, '每 ' + formatCount(data.refresh_interval_seconds) + ' 秒刷新');
  const isUp = data.overall_status === 'up';
  overallBanner.className = 'banner ' + (isUp ? 'up' : 'down');
  setText(overallBanner, isUp ? '所有系统运行正常' : '部分系统故障');
}

function showRefreshFailure() {
  refreshNotice.hidden = false;
  setText(refreshNotice, '数据暂时无法刷新');
  if (!lastSuccessfulData) {
    overallBanner.className = 'banner loading';
    setText(overallBanner, '数据暂时无法刷新');
  }
}

async function loadData() {
  if (inFlight) return;
  inFlight = true;
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  try {
    const response = await fetch('/api/status', { signal: controller.signal });
    if (!response.ok) throw new Error('status request failed');
    const data = await response.json();
    render(data);
    lastSuccessfulData = data;
    refreshNotice.hidden = true;
  } catch (_error) {
    showRefreshFailure();
  } finally {
    clearTimeout(timeoutId);
    inFlight = false;
  }
}

document.addEventListener('keydown', event => {
  if (event.key === 'Escape') dismissTooltip();
});
loadData();
setInterval(loadData, 60000);
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
