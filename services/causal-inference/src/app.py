"""Phase 6 causal-inference FastAPI sidecar.

Exposes a single ``POST /infer`` endpoint that returns a deterministic
canned root-cause for one of the demo scenarios. Phase 7 will replace the
canned lookup with a real DoWhy estimand search backed by the codegraph.

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


class RootCauseCandidate(BaseModel):
    """One candidate node the upstream Pathfinder graph search emitted."""

    node: str = Field(..., description="Fully qualified symbol or graph node id")
    evidence: list[str] = Field(default_factory=list)


class InferRequest(BaseModel):
    """Phase 6 inbound request.

    ``scenario`` is the demo-fixture label used to select a canned root cause.
    ``root_cause_candidates`` is forwarded by the Pathfinder agent so future
    phases can mix the canned answer with codegraph evidence.
    """

    root_cause_candidates: list[RootCauseCandidate] = Field(default_factory=list)
    scenario: str = Field(..., description="Demo scenario label, e.g. demo-zero-div")


class RootCause(BaseModel):
    node: str
    confidence: float
    evidence_chain: list[str] = Field(default_factory=list)


class InferResponse(BaseModel):
    root_cause: RootCause
    duration_ms: int
    scenario_matched: bool


app = FastAPI(
    title="nexis-causal-inference",
    version="0.1.0",
    description=(
        "Phase 6 canned-response causal-inference sidecar. Maps a demo "
        "scenario label to a fixed root_cause node + evidence chain."
    ),
)


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    """Liveness probe consumed by docker-compose."""

    return {"status": "ok", "service": "causal-inference"}


@app.post("/infer", response_model=InferResponse)
def infer(req: InferRequest) -> InferResponse:
    """Returns the canned root cause for the requested scenario.

    Phase 6 ignores ``root_cause_candidates`` — the canned table is the
    sole source of truth. We still echo back ``scenario_matched`` so the
    caller can distinguish a real hit from the fallback.
    """

    started = time.monotonic_ns()
    hit = lookup(req.scenario)
    matched = hit["node"] != "<unknown>"
    elapsed = (time.monotonic_ns() - started) // 1_000_000
    return InferResponse(
        root_cause=RootCause(
            node=hit["node"],
            confidence=hit["confidence"],
            evidence_chain=hit["evidence_chain"],
        ),
        duration_ms=int(elapsed),
        scenario_matched=matched,
    )


def main() -> None:
    """Entrypoint used by the Dockerfile + ``make run``."""

    import uvicorn

    port = int(os.environ.get("PORT", "8090"))
    uvicorn.run(app, host="0.0.0.0", port=port, log_level="info")


if __name__ == "__main__":
    main()
