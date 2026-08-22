"""Definitions and safe HTTP probes for the status monitor."""

from dataclasses import dataclass
import json
import socket
import time
import urllib.error
import urllib.request


@dataclass(frozen=True)
class Component:
    """Immutable configuration for one monitored component."""

    key: str
    name: str
    url: str
    kind: str

    @property
    def id(self):
        """Compatibility alias for callers that use an identifier name."""
        return self.key

    @property
    def probe_kind(self):
        """Compatibility alias for callers that spell out the probe kind."""
        return self.kind


@dataclass(frozen=True)
class ProbeResult:
    """The sanitized result exposed to the monitor and status page."""

    status: str
    code: int | None
    latency_ms: int | None
    error: str | None


def load_components(env):
    """Load components from an explicit environment mapping in display order."""
    return (
        Component("frontend", "Frontend", env["FRONTEND_URL"], "http"),
        Component("api", "API", env["API_HEALTH_URL"], "json_ok"),
        Component("worker", "Worker", env["WORKER_HEALTH_URL"], "json_ok"),
        Component("rsshub", "RSSHub", env["RSSHUB_HEALTH_URL"], "http"),
        Component("public", "公网入口", env["PUBLIC_HEALTH_URL"], "json_ok"),
    )


def _latency_ms(start):
    return max(0, int((time.monotonic() - start) * 1000))


def _response_code(response):
    code = getattr(response, "status", None)
    if code is None:
        try:
            code = response.getcode()
        except Exception:
            return None
    try:
        return int(code)
    except (TypeError, ValueError):
        return None


def _drain_and_close(response):
    body = None
    read_error = None
    try:
        body = response.read()
    except Exception as exc:
        read_error = exc
    finally:
        try:
            response.close()
        except Exception:
            pass
    return body, read_error


def _error_category(exc):
    if isinstance(exc, (TimeoutError, socket.timeout)):
        return "connection_timeout"
    if isinstance(exc, urllib.error.URLError) and isinstance(
        exc.reason, (TimeoutError, socket.timeout)
    ):
        return "connection_timeout"
    return "connection_failed"


def _down(code, latency_ms, error):
    return ProbeResult("down", code, latency_ms, error)


def probe(component, timeout=10):
    """Probe a component and return only safe, structured outcome data."""
    start = time.monotonic()
    response = None
    try:
        request = urllib.request.Request(
            component.url,
            method="GET",
            headers={"User-Agent": "rss-pal-monitor/1.0"},
        )
        response = urllib.request.urlopen(request, timeout=timeout)
        code = _response_code(response)
        body, read_error = _drain_and_close(response)
        latency_ms = _latency_ms(start)

        if read_error is not None:
            return _down(code, latency_ms, _error_category(read_error))

        if component.kind == "http":
            if code is not None and 200 <= code < 400:
                return ProbeResult("up", code, latency_ms, None)
            return _down(code, latency_ms, "http_error")

        if component.kind != "json_ok":
            return _down(code, latency_ms, "invalid_response")
        if code != 200:
            return _down(code, latency_ms, "http_error")

        try:
            payload = json.loads(body.decode("utf-8"))
        except (AttributeError, UnicodeDecodeError, json.JSONDecodeError):
            return _down(code, latency_ms, "invalid_response")
        if isinstance(payload, dict) and payload.get("status") == "ok":
            return ProbeResult("up", code, latency_ms, None)
        return _down(code, latency_ms, "invalid_response")
    except urllib.error.HTTPError as exc:
        code = _response_code(exc)
        _drain_and_close(exc)
        return _down(code, _latency_ms(start), "http_error")
    except Exception as exc:
        return _down(None, _latency_ms(start), _error_category(exc))
    finally:
        # A response is normally closed by _drain_and_close. This protects the
        # connection if an unexpected response-processing error occurs first.
        if response is not None:
            try:
                response.close()
            except Exception:
                pass


__all__ = ["Component", "ProbeResult", "load_components", "probe"]
