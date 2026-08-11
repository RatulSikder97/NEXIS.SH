"""Unit tests for the graph-evidence ranking algorithm.

Each test pins one property claimed in ranking.py's methodology
docstring: ordering sensibility, monotone graph-proximity influence,
textual corroboration, weight renormalisation on missing signals,
contrast-damped confidence bounds, and the empty/no-signal degradations.
"""

from __future__ import annotations

import math

import pytest

from ranking import (
    THRESHOLD,
    WEIGHTS,
    Candidate,
    evidence_score,
    graph_score,
    rank_candidates,
)


STACK = """Traceback (most recent call last):
  File "src/nexis_fixture/api.py", line 8, in handler
    result = safe_div(total, count)
ZeroDivisionError: division by zero"""


def strong_candidate(node: str = "nexis_fixture.api.safe_div") -> Candidate:
    return Candidate(
        node=node,
        evidence=(
            "symbol=safe_div in src/nexis_fixture/api.py:5-9",
            "handler->safe_div(CALLS)",
            "safe_div->ZeroDivisionError(RAISED)",
            "ZeroDivisionError raised when b==0 at api.py:5",
        ),
        in_degree=3,
        out_degree=1,
        distance_from_symptom=0,
    )


def weak_candidate(node: str = "nexis_fixture.api.unrelated") -> Candidate:
    return Candidate(
        node=node,
        evidence=("seen during traversal",),
        in_degree=0,
        out_degree=0,
        distance_from_symptom=2,
    )


# ---------------------------------------------------------------------------
# Ordering
# ---------------------------------------------------------------------------


def test_strong_evidence_outranks_weak() -> None:
    res = rank_candidates([weak_candidate(), strong_candidate()], incident_text=STACK)
    assert res.top is not None
    assert res.top.node == "nexis_fixture.api.safe_div"
    assert res.ranked[0].score > res.ranked[1].score
    assert res.method == "graph_evidence_ranking"


def test_ordering_is_deterministic_under_input_permutation() -> None:
    a = [strong_candidate(), weak_candidate()]
    b = [weak_candidate(), strong_candidate()]
    ra = rank_candidates(a, incident_text=STACK)
    rb = rank_candidates(b, incident_text=STACK)
    assert [c.node for c in ra.ranked] == [c.node for c in rb.ranked]
    assert ra.confidence == pytest.approx(rb.confidence)


def test_closer_to_symptom_ranks_higher_all_else_equal() -> None:
    """Proximity is monotone: same evidence, smaller hop distance wins."""

    evidence = ("symbol=f in src/a.py:1-5", "f->Boom(RAISED)")
    near = Candidate(node="near", evidence=evidence, distance_from_symptom=0)
    far = Candidate(node="far", evidence=evidence, distance_from_symptom=3)
    res = rank_candidates([far, near])
    assert [c.node for c in res.ranked] == ["near", "far"]


def test_text_corroboration_boosts_matching_candidate() -> None:
    """Same structure; only one candidate's tokens appear in the trace."""

    matching = Candidate(
        node="safe_div", evidence=("ZeroDivisionError raised in safe_div",)
    )
    other = Candidate(
        node="render_widget", evidence=("TemplateSyntaxWarning raised in render_widget",)
    )
    res = rank_candidates([other, matching], incident_text=STACK)
    assert res.top is not None
    assert res.top.node == "safe_div"


def test_scenario_prior_is_tiebreaker_not_selector() -> None:
    """A label match must not lift an unevidenced candidate over an
    evidenced one — the 0.05 weight caps its influence."""

    labelled = Candidate(node="zero.div.decoy", evidence=())
    evidenced = strong_candidate()
    res = rank_candidates([labelled, evidenced], incident_text=STACK, scenario="demo-zero-div")
    assert res.top is not None
    assert res.top.node == evidenced.node


# ---------------------------------------------------------------------------
# Confidence bounds + contrast damping
# ---------------------------------------------------------------------------


def test_confidence_always_within_unit_interval() -> None:
    cases = [
        [],
        [weak_candidate()],
        [strong_candidate()],
        [strong_candidate(), weak_candidate()],
        [strong_candidate("a"), strong_candidate("b")],  # exact tie
        [Candidate(node="empty")],
    ]
    for cands in cases:
        res = rank_candidates(cands, incident_text=STACK, scenario="demo-zero-div")
        assert 0.0 <= res.confidence <= 1.0, cands
        for scored in res.ranked:
            assert 0.0 <= scored.score <= 1.0


def test_tied_top_two_halve_confidence() -> None:
    """confidence = s1 * (1 - 0.5 * (s2/s1)^2): an exact tie halves it
    relative to the same candidate standing alone."""

    solo = rank_candidates([strong_candidate("a")], incident_text=STACK)
    tied = rank_candidates(
        [strong_candidate("a"), strong_candidate("b")], incident_text=STACK
    )
    assert tied.confidence == pytest.approx(solo.confidence * 0.5)
    assert tied.matched is (tied.confidence >= THRESHOLD)


def test_strong_separated_winner_clears_threshold() -> None:
    res = rank_candidates(
        [strong_candidate(), weak_candidate()],
        incident_text=STACK,
        scenario="demo-zero-div",
    )
    assert res.confidence >= THRESHOLD
    assert res.matched is True


# ---------------------------------------------------------------------------
# Degradations
# ---------------------------------------------------------------------------


def test_empty_candidates_returns_no_signal() -> None:
    res = rank_candidates([])
    assert res.ranked == []
    assert res.confidence == 0.0
    assert res.matched is False
    assert res.method == "no_signal"
    assert res.top is None


def test_all_zero_signal_returns_no_signal() -> None:
    res = rank_candidates([Candidate(node="aaa"), Candidate(node="bbb")])
    assert res.method == "no_signal"
    assert res.confidence == 0.0
    assert res.matched is False


def test_missing_components_renormalise_not_zero() -> None:
    """Evidence-only candidate (no graph, no incident text): the score
    must equal the evidence/prior mix, not be dragged down by absent
    components imputed as zero."""

    cand = Candidate(
        node="safe_div",
        evidence=("symbol=safe_div in src/a.py:1-5", "handler->safe_div(CALLS)"),
    )
    res = rank_candidates([cand])
    assert res.top is not None
    comps = res.top.components
    assert comps["graph"] is None
    assert comps["text"] is None
    assert comps["prior"] is None  # no scenario given
    assert res.top.score == pytest.approx(comps["evidence"])


# ---------------------------------------------------------------------------
# Component functions
# ---------------------------------------------------------------------------


def test_evidence_score_zero_without_evidence() -> None:
    assert evidence_score(Candidate(node="x")) == 0.0


def test_evidence_score_rewards_specific_anchors() -> None:
    vague = Candidate(node="x", evidence=("something looked wrong somewhere",))
    anchored = Candidate(node="x", evidence=("handler->safe_div(CALLS) at api.py:8",))
    assert evidence_score(anchored) > evidence_score(vague)


def test_evidence_score_saturates() -> None:
    """Diminishing returns: 4 -> 8 items adds less than 1 -> 4."""

    def with_n(n: int) -> float:
        return evidence_score(
            Candidate(node="x", evidence=tuple(f"edge{i}->sink(CALLS)" for i in range(n)))
        )

    assert with_n(4) - with_n(1) > with_n(8) - with_n(4)
    assert with_n(50) <= 1.0


def test_graph_score_none_without_metadata() -> None:
    assert graph_score(Candidate(node="x", evidence=("e",))) is None


def test_graph_score_prefers_proximity_over_centrality() -> None:
    """Weights 0.7/0.3: adjacent beats a distant hub."""

    adjacent = graph_score(Candidate(node="a", distance_from_symptom=0, in_degree=1, out_degree=0))
    distant_hub = graph_score(Candidate(node="b", distance_from_symptom=3, in_degree=10, out_degree=10))
    assert adjacent is not None and distant_hub is not None
    assert adjacent > distant_hub


def test_weights_sum_to_one() -> None:
    assert math.isclose(sum(WEIGHTS.values()), 1.0)
