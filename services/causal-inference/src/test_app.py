"""Tests for the Phase 6 canned causal-inference FastAPI sidecar."""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app import app
from canned import CANNED, DEFAULT, lookup


client = TestClient(app)


# ---------------------------------------------------------------------------
# canned.lookup — pure-Python coverage
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("scenario", list(CANNED.keys()))
def test_lookup_known_scenario_returns_node_and_confidence(scenario: str) -> None:
    hit = lookup(scenario)
    assert hit["node"] != DEFAULT["node"], f"{scenario} should resolve to a real node"
    assert 0.0 < hit["confidence"] <= 1.0
    assert hit["evidence_chain"], f"{scenario} must ship an evidence chain"


def test_lookup_unknown_falls_back_to_default() -> None:
    hit = lookup("does-not-exist")
    assert hit["node"] == DEFAULT["node"]
    assert hit["confidence"] == 0.0
    assert hit["evidence_chain"] == []


def test_lookup_returns_independent_evidence_chain() -> None:
    """Mutating the returned chain must not leak back into the canned table."""

    hit = lookup("demo-zero-div")
    hit["evidence_chain"].append("polluted")
    refreshed = lookup("demo-zero-div")
    assert "polluted" not in refreshed["evidence_chain"]


# ---------------------------------------------------------------------------
# FastAPI surface — the only public contract
# ---------------------------------------------------------------------------


def test_healthz_ok() -> None:
    r = client.get("/healthz")
    assert r.status_code == 200
    body = r.json()
    assert body == {"status": "ok", "service": "causal-inference"}


@pytest.mark.parametrize(
    "scenario,expected_node",
    [
        ("demo-null-pointer", "nexis_fixture.api.safe_div"),
        ("demo-zero-div", "nexis_fixture.api.safe_div"),
        ("schema-drift", "nexis_fixture.api.get_user"),
    ],
)
def test_infer_canned_scenarios(scenario: str, expected_node: str) -> None:
    r = client.post(
        "/infer",
        json={
            "scenario": scenario,
            "root_cause_candidates": [
                {"node": "ignored.by.phase6", "evidence": ["upstream"]}
            ],
        },
    )
    assert r.status_code == 200
    body = r.json()
    assert body["scenario_matched"] is True
    assert body["root_cause"]["node"] == expected_node
    assert body["root_cause"]["confidence"] > 0.5
    assert isinstance(body["root_cause"]["evidence_chain"], list)
    assert len(body["root_cause"]["evidence_chain"]) >= 1
    assert isinstance(body["duration_ms"], int)
    assert body["duration_ms"] >= 0


def test_infer_unknown_scenario_returns_unmatched() -> None:
    r = client.post(
        "/infer",
        json={"scenario": "totally-bogus", "root_cause_candidates": []},
    )
    assert r.status_code == 200
    body = r.json()
    assert body["scenario_matched"] is False
    assert body["root_cause"]["node"] == "<unknown>"
    assert body["root_cause"]["confidence"] == 0.0
    assert body["root_cause"]["evidence_chain"] == []


def test_infer_rejects_missing_scenario() -> None:
    r = client.post("/infer", json={"root_cause_candidates": []})
    assert r.status_code == 422, r.text
