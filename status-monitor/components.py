"""Definitions and safe HTTP probes for the status monitor."""

from dataclasses import dataclass
import json
import socket
import time
import urllib.error
import urllib.request


class _NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, msg, headers, newurl):
        return None


_NO_REDIRECT_OPENER = urllib.request.build_opener(_NoRedirectHandler())
_monotonic = time.monotonic
MAX_RESPONSE_BYTES = 64 * 1024
_READ_CHUNK_BYTES = 8 * 1024


class _ReadLimitExceeded(Exception):
    pass


class _ProbeDeadlineExceeded(Exception):
    pass


class _BodyLimitReached(Exception):
    pass


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
    return max(0, int((_monotonic() - start) * 1000))


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


def _set_socket_timeout(response, remaining):
    """Apply the remaining probe deadline to a urllib response socket."""
    timeout = max(0.001, remaining)
    pending = [response]
    seen = set()
    while pending:
        current = pending.pop()
        if current is None or id(current) in seen:
            continue
        seen.add(id(current))
        setter = getattr(current, "settimeout", None)
        if setter is not None:
            try:
                setter(timeout)
                return
            except (AttributeError, OSError):
                pass
        for attr in ("fp", "raw", "_sock", "sock", "socket"):
            try:
                pending.append(getattr(current, attr, None))
            except Exception:
                pass


def _read_body(response, deadline, enforce_limit):
    chunks = []
    total = 0
    reader = getattr(response, "read1", None) or response.read
    while True:
        if _monotonic() >= deadline:
            raise _ProbeDeadlineExceeded
        _set_socket_timeout(response, deadline - _monotonic())
        chunk = reader(min(_READ_CHUNK_BYTES, MAX_RESPONSE_BYTES + 1 - total))
        if not chunk:
            if _monotonic() >= deadline:
                raise _ProbeDeadlineExceeded
            return b"".join(chunks)
        total += len(chunk)
        if total > MAX_RESPONSE_BYTES:
            raise _ReadLimitExceeded if enforce_limit else _BodyLimitReached
        chunks.append(chunk)
        if _monotonic() >= deadline:
            raise _ProbeDeadlineExceeded


def _drain_and_close(response, deadline, enforce_limit):
    body = None
    read_error = None
    try:
        body = _read_body(response, deadline, enforce_limit)
    except Exception as exc:
        read_error = exc
    finally:
        try:
            response.close()
        except Exception:
            pass
    return body, read_error


def _error_category(exc):
    if isinstance(exc, _ProbeDeadlineExceeded):
        return "connection_timeout"
    if isinstance(exc, _ReadLimitExceeded):
        return "invalid_response"
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
    start = _monotonic()
    deadline = start + timeout
    response = None
    try:
        request = urllib.request.Request(
            component.url,
            method="GET",
            headers={"User-Agent": "rss-pal-monitor/1.0"},
        )
        response = _NO_REDIRECT_OPENER.open(request, timeout=timeout)
        code = _response_code(response)
        body, read_error = _drain_and_close(
            response, deadline, component.kind == "json_ok"
        )
        latency_ms = _latency_ms(start)

        if component.kind == "json_ok" and code != 200:
            return _down(code, latency_ms, "http_error")
        if isinstance(read_error, _BodyLimitReached):
            read_error = None
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
        _, read_error = _drain_and_close(
            exc, deadline, component.kind == "json_ok"
        )
        latency_ms = _latency_ms(start)
        if component.kind == "json_ok" and code != 200:
            return _down(code, latency_ms, "http_error")
        if isinstance(read_error, _BodyLimitReached):
            read_error = None
        if read_error is not None:
            return _down(code, latency_ms, _error_category(read_error))
        if component.kind == "http" and code is not None and 200 <= code < 400:
            return ProbeResult("up", code, latency_ms, None)
        return _down(code, latency_ms, "http_error")
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
