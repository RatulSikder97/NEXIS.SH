"""Tests for the causal-inference FastAPI sidecar.

Covers both inference paths:

* graph_evidence_ranking — ``root_cause_candidates`` present: the
  ranking module selects the answer; the scenario label must NOT be
  able to override an evidenced candidate.
* scenario_prior_fallback — no candidates: the demo prior table in
  ``canned.py`` answers, honestly labelled as a fallback.

The ranking algorithm itself is unit-tested in tests/test_ranking.py;
this file pins the HTTP wire contract.
"""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from app import app
from canned import CANNED, DEFAULT, lookup


client = TestClient(app)


ZERO_DIV_STACK = """Traceback (most recent call last):
  File "src/nexis_fixture/api.py", line 8, in handler
    result = safe_div(total, count)
  File "src/nexis_fixture/api.py", line 5, in safe_div
    return a / b
ZeroDivisionError: division by zero"""

#: Pathfinder-shaped candidates: the true root cause carries specific,
#: corroborated evidence + close graph position; the decoy is vague.
ZERO_DIV_CANDIDATES = [
    {
        "node": "nexis_fixture.api.safe_div",
        "evidence": [
            "symbol=safe_div in src/nexis_fixture/api.py:5-9",
            "handler->safe_div(CALLS)",
            "safe_div->ZeroDivisionError(RAISED)",
            "ZeroDivisionError raised when b==0 at api.py:5",
        ],
        "in_degree": 3,
        "out_degree": 1,
        "distance_from_symptom": 0,
    },
    {
        "node": "nexis_fixture.api.unrelated_helper",
        "evidence": ["seen during traversal"],
        "in_degree": 0,
        "out_degree": 0,
        "distance_from_symptom": 2,
    },
]


# ---------------------------------------------------------------------------
# canned.lookup — fallback table coverage (kept: canned.py still backs the
# empty-candidates path)
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


def test_infer_ranks_candidates_not_scenario_label() -> None:
    """Primary path: the evidenced candidate wins on its own merits."""

    r = client.post(
        "/infer",
        json={
            "scenario": "demo-zero-div",
            "incident_text": ZERO_DIV_STACK,
            "root_cause_candidates": ZERO_DIV_CANDIDATES,
        },
    )
    assert r.status_code == 200
    body = r.json()
    assert body["method"] == "graph_evidence_ranking"
    assert body["root_cause"]["node"] == "nexis_fixture.api.safe_div"
    assert body["scenario_matched"] is True
    assert 0.5 <= body["root_cause"]["confidence"] <= 1.0
    assert len(body["root_cause"]["evidence_chain"]) == 4
    assert isinstance(body["duration_ms"], int)
    assert body["duration_ms"] >= 0
    # Ranked list is transparent: both hypotheses present, ordered,
    # with per-component score breakdowns.
    ranked = body["ranked_candidates"]
    assert [c["node"] for c in ranked] == [
        "nexis_fixture.api.safe_div",
        "nexis_fixture.api.unrelated_helper",
    ]
    assert ranked[0]["score"] > ranked[1]["score"]
    assert set(ranked[0]["components"]) == {"evidence", "graph", "text", "prior"}


def test_infer_scenario_label_cannot_override_candidates() -> None:
    """A mismatched label must not drag the answer to the canned node."""

    r = client.post(
        "/infer",
        json={
            "scenario": "schema-drift",  # canned would say get_user
            "incident_text": ZERO_DIV_STACK,
            "root_cause_candidates": ZERO_DIV_CANDIDATES,
        },
    )
    assert r.status_code == 200
    body = r.json()
    assert body["root_cause"]["node"] == "nexis_fixture.api.safe_div"
    assert body["method"] == "graph_evidence_ranking"


def test_infer_candidates_without_graph_metadata_still_rank() -> None:
    """Graph-less deployments send evidence-only candidates; weight
    renormalisation must keep them rankable rather than zeroed."""

    r = client.post(
        "/infer",
        json={
            "scenario": "demo-zero-div",
            "incident_text": ZERO_DIV_STACK,
            "root_cause_candidates": [
                {
                    "node": "nexis_fixture.api.safe_div",
                    "evidence": [
                        "symbol=safe_div in src/nexis_fixture/api.py:5-9",
                        "safe_div->ZeroDivisionError(RAISED)",
                        "ZeroDivisionError raised when b==0 at api.py:5",
                    ],
                }
            ],
        },
    )
    body = r.json()
    assert body["method"] == "graph_evidence_ranking"
    assert body["root_cause"]["node"] == "nexis_fixture.api.safe_div"
    assert body["root_cause"]["confidence"] > 0.0
    assert body["ranked_candidates"][0]["components"]["graph"] is None


def test_infer_zero_signal_candidates_return_unknown() -> None:
    """Candidates with no evidence, no graph data and no text overlap
    must not fabricate a winner."""

    r = client.post(
        "/infer",
        json={
            "scenario": "totally-bogus",
            "root_cause_candidates": [
                {"node": "aaa"},
                {"node": "bbb"},
            ],
        },
    )
    body = r.json()
    assert body["method"] == "no_signal"
    assert body["scenario_matched"] is False
    assert body["root_cause"]["node"] == "<unknown>"
    assert body["root_cause"]["confidence"] == 0.0


@pytest.mark.parametrize(
    "scenario,expected_node",
    [
        ("demo-null-pointer", "nexis_fixture.api.safe_div"),
        ("demo-zero-div", "nexis_fixture.api.safe_div"),
        ("schema-drift", "nexis_fixture.api.get_user"),
    ],
)
def test_infer_empty_candidates_uses_prior_fallback(
    scenario: str, expected_node: str
) -> None:
    """No candidates -> demo prior table, labelled as such."""

    r = client.post(
        "/infer",
        json={"scenario": scenario, "root_cause_candidates": []},
    )
    assert r.status_code == 200
    body = r.json()
    assert body["method"] == "scenario_prior_fallback"
    assert body["scenario_matched"] is True
    assert body["root_cause"]["node"] == expected_node
    assert body["root_cause"]["confidence"] > 0.5
    assert len(body["root_cause"]["evidence_chain"]) >= 1
    assert body["ranked_candidates"] == []


def test_infer_unknown_scenario_returns_unmatched() -> None:
    r = client.post(
        "/infer",
        json={"scenario": "totally-bogus", "root_cause_candidates": []},
    )
    assert r.status_code == 200
    body = r.json()
    assert body["method"] == "no_signal"
    assert body["scenario_matched"] is False
    assert body["root_cause"]["node"] == "<unknown>"
    assert body["root_cause"]["confidence"] == 0.0
    assert body["root_cause"]["evidence_chain"] == []


def test_infer_rejects_missing_scenario() -> None:
    r = client.post("/infer", json={"root_cause_candidates": []})
    assert r.status_code == 422, r.text


def test_infer_rejects_negative_graph_metadata() -> None:
    r = client.post(
        "/infer",
        json={
            "scenario": "x",
            "root_cause_candidates": [
                {"node": "n", "distance_from_symptom": -1}
            ],
        },
    )
    assert r.status_code == 422, r.text
