"""Healthcheck contract tests for the causal-inference sidecar.

These tests pin down the wire contract that the docker-compose HTTP
healthcheck (``curl -fsS http://localhost:8090/healthz``) relies on.
If any assertion in this file breaks, the compose healthcheck will
flip to ``unhealthy`` in production and the control-plane's
``depends_on`` chain will stall — so changes here are load-bearing.

We deliberately keep these tests independent of ``src/test_app.py``:
the sibling tests cover the broader API surface, while this file is
the single source of truth for the liveness probe.
"""

from __future__ import annotations

from fastapi.testclient import TestClient

from app import app


client = TestClient(app)


def test_healthz_returns_200() -> None:
    """The healthcheck endpoint must respond 200 OK.

    docker-compose treats any non-zero curl exit as an unhealthy probe,
    so 2xx is the only acceptable response class here.
    """

    r = client.get("/healthz")
    assert r.status_code == 200, r.text


def test_healthz_returns_json_status_ok() -> None:
    """The body must be JSON-shaped with ``status: ok``.

    The control-plane logs this body when it polls the sidecar from
    Pathfinder bootstrap, so the shape is part of the public contract.
    """

    r = client.get("/healthz")
    body = r.json()
    assert body.get("status") == "ok", body


def test_healthz_identifies_service() -> None:
    """The body must self-identify as the causal-inference service.

    The control-plane uses ``service`` to disambiguate sidecar logs
    when multiple healthchecks share a log stream during stack boot.
    """

    r = client.get("/healthz")
    body = r.json()
    assert body.get("service") == "causal-inference", body


def test_healthz_is_idempotent() -> None:
    """Repeated probes must return identical bodies.

    docker-compose probes once every 10s; any state-mutating side
    effect would surface here as a drift between consecutive bodies.
    """

    first = client.get("/healthz").json()
    second = client.get("/healthz").json()
    assert first == second


def test_healthz_accepts_only_get() -> None:
    """The probe is GET-only — POST/PUT/DELETE must not be routed here.

    FastAPI emits 405 Method Not Allowed when a path exists but the
    verb doesn't. Anything outside the {200, 405} pair would signal a
    routing regression that could mask real outages.
    """

    r = client.post("/healthz")
    assert r.status_code == 405, r.text
