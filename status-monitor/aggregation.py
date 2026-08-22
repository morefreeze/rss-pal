"""Server-side component history aggregation for the status API.

All returned timestamps are ISO 8601 values normalized to China Standard Time
(``+08:00``).  Input check rows may be SQLite result tuples containing either
``(ts, status, code, latency_ms, error)``, a six-column row with ``source``,
or the complete checks schema including its leading ``id``.
"""

from collections.abc import Mapping
from datetime import datetime, timedelta, timezone


CST = timezone(timedelta(hours=8))


def hour_floor(value: datetime) -> datetime:
    """Normalize an aware datetime to the beginning of its CST natural hour."""
    if value.tzinfo is None or value.utcoffset() is None:
        raise ValueError("value must be timezone-aware")
    return value.astimezone(CST).replace(minute=0, second=0, microsecond=0)


def _parse_timestamp(value) -> datetime:
    parsed = datetime.fromisoformat(value) if isinstance(value, str) else value
    if not isinstance(parsed, datetime):
        raise TypeError("check timestamp must be a datetime or ISO 8601 string")
    if parsed.tzinfo is None or parsed.utcoffset() is None:
        raise ValueError("check timestamp must be timezone-aware")
    return parsed.astimezone(CST)


def _record(row):
    if isinstance(row, Mapping) or hasattr(row, "keys"):
        return {
            "ts": _parse_timestamp(row["ts"]),
            "status": row["status"],
            "code": row["code"],
            "latency_ms": row["latency_ms"],
            "error": row["error"],
        }
    if len(row) == 5:
        ts, status, code, latency_ms, error = row
    elif len(row) == 6:
        ts, _source, status, code, latency_ms, error = row
    elif len(row) == 7:
        _id, ts, _source, status, code, latency_ms, error = row
    else:
        raise ValueError("check row must contain five, six, or seven fields")
    return {
        "ts": _parse_timestamp(ts),
        "status": status,
        "code": code,
        "latency_ms": latency_ms,
        "error": error,
    }


def _window(now: datetime, hours: int):
    if hours < 1:
        raise ValueError("hours must be positive")
    current_hour = hour_floor(now)
    return current_hour - timedelta(hours=hours - 1), current_hour + timedelta(hours=1)


def _records_in_window(rows, now: datetime, hours: int, ordered: bool = False):
    start, end = _window(now, hours)
    records = [
        record for row in rows if start <= (record := _record(row))["ts"] < end
    ]
    return records if ordered else sorted(records, key=lambda record: record["ts"])


def _bucket(start: datetime, records: list[dict]) -> dict:
    total = len(records)
    successful = sum(record["status"] == "up" for record in records)
    latencies = [record["latency_ms"] for record in records if record["latency_ms"] is not None]
    down_records = [record for record in records if record["status"] != "up"]
    last_down = down_records[-1] if down_records else None
    return {
        "start": start.isoformat(),
        "end": (start + timedelta(hours=1)).isoformat(),
        "status": None if not total else ("up" if successful == total else "down"),
        "uptime_pct": None if not total else round(successful / total * 100, 2),
        "successful_checks": successful,
        "total_checks": total,
        "avg_latency_ms": round(sum(latencies) / len(latencies)) if latencies else None,
        "last_error": last_down["error"] if last_down else None,
        "last_error_at": last_down["ts"].isoformat() if last_down else None,
    }


def _build_hour_buckets(records, now: datetime, hours: int) -> list[dict]:
    first_hour, _ = _window(now, hours)
    grouped = {first_hour + timedelta(hours=index): [] for index in range(hours)}
    for record in records:
        grouped[hour_floor(record["ts"])].append(record)
    return [_bucket(start, grouped[start]) for start in grouped]


def build_hour_buckets(rows, now: datetime, hours: int = 72) -> list[dict]:
    """Aggregate check rows into CST natural-hour buckets, oldest first."""
    return _build_hour_buckets(_records_in_window(rows, now, hours), now, hours)


def component_summary(conn, component, now: datetime, hours: int = 72) -> dict:
    """Return one configured component's current state and compact 72-hour history."""
    window_start, window_end = _window(now, hours)
    rows = conn.execute(
        "SELECT ts, status, code, latency_ms, error FROM checks "
        "WHERE source = ? AND ts >= ? AND ts < ? ORDER BY ts ASC",
        (component.key, window_start.isoformat(), window_end.isoformat()),
    ).fetchall()
    records = _records_in_window(rows, now, hours, ordered=True)
    latest = records[-1] if records else None
    successful = sum(record["status"] == "up" for record in records)
    return {
        "key": component.key,
        "name": component.name,
        "current_status": "up" if latest and latest["status"] == "up" else "down",
        "uptime_pct": round(successful / len(records) * 100, 2) if records else 0,
        "last_check": latest["ts"].isoformat() if latest else None,
        "hours": _build_hour_buckets(records, now, hours),
    }


def status_payload(conn, component_defs, now: datetime, interval_seconds) -> dict:
    """Build the public status payload without exposing component URLs or raw rows."""
    generated_at = _parse_timestamp(now).isoformat()
    components = [component_summary(conn, component, now) for component in component_defs]
    return {
        "generated_at": generated_at,
        "refresh_interval_seconds": interval_seconds,
        "overall_status": "up" if bool(components) and all(
            component["current_status"] == "up" for component in components
        ) else "down",
        "components": components,
    }


__all__ = ["CST", "hour_floor", "build_hour_buckets", "component_summary", "status_payload"]
