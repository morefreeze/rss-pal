#!/usr/bin/env python3
"""External domain checker - runs on host via cron for accurate latency."""

import http.client
import os
import sqlite3
import ssl
import time
import urllib.error
from datetime import datetime, timezone, timedelta

DB_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), "domain-checks.db")
DOMAIN = os.getenv("MONITOR_DOMAIN", "freezemacbook-pro.tailf22f5.ts.net")
CST = timezone(timedelta(hours=8))


def init_db():
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


def record_check(status, code=None, latency_ms=None, error=None):
    conn = sqlite3.connect(DB_PATH)
    conn.execute(
        "INSERT INTO checks (ts, source, status, code, latency_ms, error) VALUES (?, ?, ?, ?, ?, ?)",
        (datetime.now(CST).isoformat(), "domain", status, code, latency_ms, error)
    )
    cutoff = (datetime.now(CST) - timedelta(days=7)).isoformat()
    conn.execute("DELETE FROM checks WHERE ts < ?", (cutoff,))
    conn.commit()
    conn.close()


def do_check():
    ctx = ssl.create_default_context()
    conn = http.client.HTTPSConnection(DOMAIN, 443, context=ctx, timeout=10)
    try:
        start = time.monotonic()
        conn.request("GET", "/", headers={"User-Agent": "rss-pal-monitor/1.0"})
        resp = conn.getresponse()
        resp.read()  # drain for potential reuse
        latency = int((time.monotonic() - start) * 1000)
        record_check("up", code=resp.status, latency_ms=latency)
    except Exception as e:
        latency = int((time.monotonic() - start) * 1000) if 'start' in dir() else None
        record_check("down", error=str(e)[:200])
    finally:
        conn.close()


if __name__ == "__main__":
    init_db()
    do_check()
