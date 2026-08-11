"""Demo-fixture scenario prior — the empty-candidates fallback only.

The primary inference path is ``ranking.rank_candidates`` over the
``root_cause_candidates`` the Pathfinder forwards from its Neo4j
traversal; this table is consulted *only* when a request carries no
candidates at all (graph adapter down, or a bare demo invocation), and
responses served from it are labelled ``method="scenario_prior_fallback"``
so nothing downstream can mistake a prior lookup for ranked inference.

Each entry maps a demo scenario label (mirroring the fixture incident
labels under ``services/validator/fixtures/incidents/``) to a
deterministic root-cause hypothesis + evidence chain. Keep the keys in
sync with the fixture JSON files. The default returns a zero-confidence
"unknown" answer so callers can still short-circuit the agent chain.
"""

from __future__ import annotations

from typing import TypedDict


class CannedRootCause(TypedDict):
    node: str
    confidence: float
    evidence_chain: list[str]


# Scenario label -> canned root cause. The labels match the demo fixtures
# at services/validator/fixtures/incidents/*.json so the Pathfinder agent
# can pass the incident label straight through.
CANNED: dict[str, CannedRootCause] = {
    "demo-null-pointer": {
        "node": "nexis_fixture.api.safe_div",
        "confidence": 0.82,
        "evidence_chain": [
            "safe_div returns None on zero divisor",
            "caller code dereferences attribute on the None result",
            "graph edge: handler.predict -> calls -> safe_div",
        ],
    },
    "demo-zero-div": {
        "node": "nexis_fixture.api.safe_div",
        "confidence": 0.88,
        "evidence_chain": [
            "ZeroDivisionError raised when b==0",
            "safe_div lacks the b==0 short-circuit branch",
            "graph edge: handler -> calls -> safe_div (line 8)",
        ],
    },
    "schema-drift": {
        "node": "nexis_fixture.api.get_user",
        "confidence": 0.91,
        "evidence_chain": [
            "users.email_verified_at column missing in staging",
            "ORM query references the dropped column",
            "graph edge: get_user -> reads -> users.email_verified_at",
        ],
    },
}


DEFAULT: CannedRootCause = {
    "node": "<unknown>",
    "confidence": 0.0,
    "evidence_chain": [],
}


def lookup(scenario: str) -> CannedRootCause:
    """Returns the canned entry for ``scenario`` or DEFAULT if missing.

    Always returns a new dict so callers can mutate ``evidence_chain``
    without polluting the table.
    """

    hit = CANNED.get(scenario)
    if hit is None:
        return {"node": DEFAULT["node"], "confidence": DEFAULT["confidence"], "evidence_chain": []}
    return {
        "node": hit["node"],
        "confidence": hit["confidence"],
        "evidence_chain": list(hit["evidence_chain"]),
    }
