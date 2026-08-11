"""Causal-inference FastAPI sidecar.

Exposes ``POST /infer``: ranks the root-cause candidates forwarded by
the Pathfinder agent's Neo4j graph traversal and returns the top-ranked
hypothesis with a documented confidence score (see ``ranking`` module
docstring for the full methodology — evidence support, graph-structural
signal, textual corroboration, weak scenario prior).

The scenario label is *not* the selector: candidates are ranked on
their own evidence, and the label only contributes a 0.05-weight
tiebreaker prior. Only when the request carries no candidates at all
does the service degrade to the small demo-fixture prior table in
``canned.py`` (``method="scenario_prior_fallback"``), so a graph-less
deployment still returns something useful and honestly labelled.

Run locally::

    PYTHONPATH=src uvicorn app:app --host 0.0.0.0 --port 8090

The request/response wire format is intentionally minimal so the
Pathfinder agent and tests can talk to it without a generated client.
"""

from __future__ import annotations

import os
import time
from typing import Any

from fastapi import FastAPI
from pydantic import BaseModel, Field

from canned import lookup
from ranking import THRESHOLD, Candidate, rank_candidates


class RootCauseCandidate(BaseModel):
    """One candidate node the upstream Pathfinder graph search emitted.

    The three graph-position fields are computed by the Go Pathfinder
    from its Neo4j traversal (see control-plane
    internal/adapter/agents/pathfinder/provider.go). They describe the
    node's position *within the retrieved <=k-hop evidence subgraph*,
    not the global codegraph, and are optional so graph-less
    deployments can still submit evidence-only candidates.
    """

    node: str = Field(..., description="Fully qualified symbol or graph node id")
    evidence: list[str] = Field(default_factory=list)
    in_degree: int | None = Field(
        default=None, ge=0,
        description="Edges into this node within the retrieved evidence subgraph",
    )
    out_degree: int | None = Field(
        default=None, ge=0,
        description="Edges out of this node within the retrieved evidence subgraph",
    )
    distance_from_symptom: int | None = Field(
        default=None, ge=0,
        description="Hop count from the crashing frame's symbol (0 = the symbol itself)",
    )


class InferRequest(BaseModel):
    """Inbound inference request.

    ``root_cause_candidates`` is the primary signal — the ranked output
    is computed from it. ``incident_text`` (raw stack trace / error
    message) enables the textual-corroboration component.  ``scenario``
    is a weak prior/tiebreaker and the key for the empty-candidates
    fallback table; it never selects the answer when candidates exist.
    """

    root_cause_candidates: list[RootCauseCandidate] = Field(default_factory=list)
    scenario: str = Field(..., description="Demo scenario label, e.g. demo-zero-div")
    incident_text: str = Field(
        default="",
        description="Raw stacktrace / error text for token-overlap corroboration",
    )


class RankedHypothesis(BaseModel):
    """One entry of the transparent ranked hypothesis list.

    ``components`` exposes the per-signal breakdown (evidence / graph /
    text / prior, each in [0,1] or null when unavailable) so the UI and
    the eval harness can audit *why* a candidate won.
    """

    node: str
    score: float
    components: dict[str, float | None] = Field(default_factory=dict)


class RootCause(BaseModel):
    node: str
    confidence: float
    evidence_chain: list[str] = Field(default_factory=list)


class InferResponse(BaseModel):
    root_cause: RootCause
    duration_ms: int
    scenario_matched: bool
    method: str = Field(
        default="no_signal",
        description=(
            "graph_evidence_ranking | scenario_prior_fallback | no_signal — "
            "which path produced root_cause"
        ),
    )
    ranked_candidates: list[RankedHypothesis] = Field(default_factory=list)


app = FastAPI(
    title="nexis-causal-inference",
    version="0.2.0",
    description=(
        "Causal-inference sidecar: ranks Pathfinder's Neo4j-derived "
        "root-cause candidates via a documented graph-evidence scoring "
        "function and returns the top hypothesis with confidence."
    ),
)


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    """Liveness probe consumed by docker-compose."""

    return {"status": "ok", "service": "causal-inference"}


@app.post("/infer", response_model=InferResponse)
def infer(req: InferRequest) -> InferResponse:
    """Ranks root_cause_candidates; falls back to the demo prior table.

    Primary path: ``ranking.rank_candidates`` scores every candidate on
    evidence support, graph position, textual corroboration and the
    weak scenario prior; the winner's contrast-damped confidence must
    clear ``ranking.THRESHOLD`` for ``scenario_matched=True``.

    Fallback path (``root_cause_candidates`` empty): the canned
    demo-fixture prior in ``canned.py``, honestly labelled
    ``method="scenario_prior_fallback"``. Its ``scenario_matched`` uses
    the same threshold comparison — an unknown scenario yields the
    zero-confidence ``<unknown>`` default and ``matched=False``.
    """

    started = time.monotonic_ns()

    if req.root_cause_candidates:
        result = rank_candidates(
            (
                Candidate(
                    node=c.node,
                    evidence=tuple(c.evidence),
                    in_degree=c.in_degree,
                    out_degree=c.out_degree,
                    distance_from_symptom=c.distance_from_symptom,
                )
                for c in req.root_cause_candidates
            ),
            incident_text=req.incident_text,
            scenario=req.scenario,
        )
        ranked = [
            RankedHypothesis(node=s.node, score=s.score, components=s.components)
            for s in result.ranked
        ]
        top = result.top
        if result.method == "no_signal" or top is None:
            root = RootCause(node="<unknown>", confidence=0.0, evidence_chain=[])
        else:
            root = RootCause(
                node=top.node,
                confidence=round(result.confidence, 4),
                evidence_chain=top.evidence_chain,
            )
        elapsed = (time.monotonic_ns() - started) // 1_000_000
        return InferResponse(
            root_cause=root,
            duration_ms=int(elapsed),
            scenario_matched=result.matched,
            method=result.method,
            ranked_candidates=ranked,
        )

    # ----- Fallback: no candidates to rank — demo-fixture prior table.
    hit = lookup(req.scenario)
    matched = hit["confidence"] >= THRESHOLD
    method = "scenario_prior_fallback" if hit["node"] != "<unknown>" else "no_signal"
    elapsed = (time.monotonic_ns() - started) // 1_000_000
    return InferResponse(
        root_cause=RootCause(
            node=hit["node"],
            confidence=hit["confidence"],
            evidence_chain=hit["evidence_chain"],
        ),
        duration_ms=int(elapsed),
        scenario_matched=matched,
        method=method,
        ranked_candidates=[],
    )


def main() -> None:
    """Entrypoint used by the Dockerfile + ``make run``."""

    import uvicorn

    port = int(os.environ.get("PORT", "8090"))
    uvicorn.run(app, host="0.0.0.0", port=port, log_level="info")


if __name__ == "__main__":
    main()
