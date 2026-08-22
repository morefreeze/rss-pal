import json
import sqlite3
import unittest
from datetime import datetime, timedelta, timezone

from aggregation import (
    CST,
    build_hour_buckets,
    component_summary,
    hour_floor,
    status_payload,
)
from components import Component


class AggregationTests(unittest.TestCase):
    def setUp(self):
        self.conn = sqlite3.connect(":memory:")
        self.conn.execute(
            "CREATE TABLE checks (ts TEXT, source TEXT, status TEXT, code INTEGER, "
            "latency_ms INTEGER, error TEXT)"
        )
        self.now = datetime(2026, 8, 22, 12, 34, 56, tzinfo=CST)

    def tearDown(self):
        self.conn.close()

    def add_check(self, timestamp, source="api", status="up", latency_ms=42, error=None):
        self.conn.execute(
            "INSERT INTO checks VALUES (?, ?, ?, ?, ?, ?)",
            (timestamp.isoformat(), source, status, 200, latency_ms, error),
        )
        self.conn.commit()

    def test_hour_floor_converts_aware_values_to_cst(self):
        value = datetime(2026, 8, 22, 4, 34, 56, 123, tzinfo=timezone.utc)
        self.assertEqual(hour_floor(value), datetime(2026, 8, 22, 12, tzinfo=CST))

    def test_builds_exactly_72_cst_natural_hour_buckets_oldest_to_newest(self):
        earliest = datetime(2026, 8, 19, 13, tzinfo=CST)
        rows = [(earliest.isoformat(), "up", 200, 42, None)]

        buckets = build_hour_buckets(rows, self.now)

        self.assertEqual(len(buckets), 72)
        self.assertEqual(buckets[0]["start"], "2026-08-19T13:00:00+08:00")
        self.assertEqual(buckets[-1]["start"], "2026-08-22T12:00:00+08:00")
        self.assertEqual(buckets[-1]["end"], "2026-08-22T13:00:00+08:00")
        self.assertEqual(buckets[0]["status"], "up")

    def test_sample_exactly_on_hour_boundary_belongs_to_that_hour(self):
        rows = [("2026-08-22T12:00:00+08:00", "up", 200, 10, None)]

        bucket = build_hour_buckets(rows, self.now)[-1]

        self.assertEqual(bucket["total_checks"], 1)
        self.assertEqual(bucket["status"], "up")

    def test_any_failure_makes_the_hour_down(self):
        rows = [
            ("2026-08-22T12:01:00+08:00", "up", 200, 10, None),
            ("2026-08-22T12:02:00+08:00", "down", 500, 20, "http_error"),
        ]

        bucket = build_hour_buckets(rows, self.now)[-1]

        self.assertEqual(bucket["status"], "down")
        self.assertEqual(bucket["successful_checks"], 1)
        self.assertEqual(bucket["total_checks"], 2)

    def test_hour_statistics_include_sample_weighted_uptime_latency_and_latest_error(self):
        rows = []
        hour = datetime(2026, 8, 22, 12, tzinfo=CST)
        for second in range(59):
            rows.append(((hour + timedelta(seconds=second)).isoformat(), "up", 200, 42, None))
        last_error_at = hour + timedelta(minutes=59)
        rows.append((last_error_at.isoformat(), "down", 500, 42, "http_error"))

        bucket = build_hour_buckets(rows, self.now)[-1]

        self.assertEqual(bucket["uptime_pct"], 98.33)
        self.assertEqual(bucket["avg_latency_ms"], 42)
        self.assertEqual(bucket["last_error"], "http_error")
        self.assertEqual(bucket["last_error_at"], "2026-08-22T12:59:00+08:00")

    def test_missing_hours_are_gray_and_excluded_from_component_uptime(self):
        self.add_check(self.now - timedelta(hours=2), status="up")
        self.add_check(self.now - timedelta(hours=1), status="down", error="http_error")

        summary = component_summary(
            self.conn, Component("api", "API", "http://private.example/health", "json_ok"), self.now
        )

        self.assertEqual(summary["uptime_pct"], 50.0)
        self.assertIsNone(summary["hours"][0]["status"])
        self.assertEqual(summary["hours"][0]["total_checks"], 0)

    def test_component_summary_uses_latest_actual_sample_for_current_status(self):
        self.add_check(self.now - timedelta(minutes=10), status="down", error="http_error")
        self.add_check(self.now - timedelta(minutes=1), status="up")

        summary = component_summary(
            self.conn, Component("api", "API", "http://private.example/health", "json_ok"), self.now
        )

        self.assertEqual(summary["current_status"], "up")
        self.assertEqual(summary["last_check"], (self.now - timedelta(minutes=1)).isoformat())

    def test_payload_preserves_component_order_hides_urls_and_derives_overall_status(self):
        self.add_check(self.now - timedelta(minutes=1), "api", "up")
        self.add_check(self.now - timedelta(minutes=1), "worker", "down", error="http_error")
        components = (
            Component("worker", "Worker", "http://worker/private", "json_ok"),
            Component("api", "API", "http://api/private", "json_ok"),
        )

        payload = status_payload(self.conn, components, self.now, interval_seconds=60)

        self.assertEqual(list(payload), ["generated_at", "refresh_interval_seconds", "overall_status", "components"])
        self.assertEqual(payload["generated_at"], self.now.isoformat())
        self.assertEqual(payload["refresh_interval_seconds"], 60)
        self.assertEqual(payload["overall_status"], "down")
        self.assertEqual([item["key"] for item in payload["components"]], ["worker", "api"])
        self.assertNotIn("url", json.dumps(payload, ensure_ascii=False))

    def test_no_samples_never_yield_green(self):
        component = Component("api", "API", "http://api/private", "json_ok")

        summary = component_summary(self.conn, component, self.now)
        payload = status_payload(self.conn, (component,), self.now, interval_seconds=60)

        self.assertEqual(summary["current_status"], "down")
        self.assertIsNone(summary["last_check"])
        self.assertEqual(summary["uptime_pct"], 0)
        self.assertEqual(payload["overall_status"], "down")

    def test_sqlite_errors_propagate(self):
        component = Component("api", "API", "http://api/private", "json_ok")
        self.conn.close()

        with self.assertRaises(sqlite3.ProgrammingError):
            component_summary(self.conn, component, self.now)


if __name__ == "__main__":
    unittest.main()
