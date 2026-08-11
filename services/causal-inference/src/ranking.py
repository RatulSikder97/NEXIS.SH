"""Graph-evidence root-cause ranking for the causal-inference sidecar.

Methodology
===========

The Pathfinder agent walks the Neo4j codegraph outward from the crash
frame and forwards a set of *root-cause candidates* — graph nodes plus
the evidence collected while traversing (call/raise edges, symbol
locations, stack-trace tokens). This module ranks those candidates and
assigns each a confidence in ``[0, 1]``.

Why not a fitted structural causal model? A full do-calculus treatment
(DoWhy-style) requires either interventional data or an observational
dataset rich enough to fit a causal DAG and estimate effects of
``do(X)`` interventions. This system has neither: incidents are sparse,
one-shot events over a synthetic fixture repo, so any "fitted" model
would be fiction. Instead we score candidates with an interpretable
evidence-combination function whose components are each a defensible
*proxy* for causal relevance, and we are explicit about that framing:

1. **Evidence support** (weight 0.35) — how much traversal evidence
   backs the candidate, with diminishing returns (a saturating
   exponential over evidence count), scaled by *specificity*: evidence
   strings that carry verifiable anchors (file:line spans, explicit
   graph edges ``a->b``, CamelCase exception classes, dotted symbol
   paths) count for more than free text, because they can be checked
   against the repo and the graph.

2. **Graph-structural signal** (weight 0.30) — position of the node in
   the retrieved evidence subgraph:

   * *Proximity to the symptom*, ``1 / (1 + d)`` where ``d`` is the hop
     distance from the crashing frame's symbol. Fault propagation over
     call edges decays quickly (most defects manifest within 1-2 frames
     of the crash site), and absent a fitted propagation model a
     harmonic decay is the least-committal monotone prior. In
     do-calculus terms: intervening on nodes nearer the symptom is a
     priori more likely to toggle the observed failure.
   * *Degree centrality*, ``log1p(in + out) / log1p(20)`` capped at 1
     (20 is the Pathfinder traversal edge limit). A symbol with more
     incident edges in the evidence subgraph has more causal pathways
     through which a defect could produce the symptom. The log damps
     hub domination; this is a centrality proxy, not a causal effect
     estimate, and is deliberately the minority share (0.3) of the
     graph component versus proximity (0.7).

   Degrees and distances are computed by the Go Pathfinder from its
   *real* Neo4j traversal and refer to the retrieved <=k-hop evidence
   subgraph, not the global graph — documented on the wire fields.

3. **Textual corroboration** (weight 0.30) — overlap coefficient
   ``|A ∩ B| / min(|A|, |B|)`` between identifier tokens drawn from the
   candidate (node name + evidence strings) and tokens from the raw
   incident text (stack trace / error message). A candidate whose
   evidence independently re-derives the tokens present in the failure
   (exception class, symbol, file) is corroborated by a second signal
   source. Overlap coefficient is preferred over Jaccard because the
   two token sets have very different sizes (a stack trace is much
   longer than an evidence chain) and Jaccard would punish that
   asymmetry.

4. **Scenario prior** (weight 0.05) — token overlap between the demo
   scenario label and the candidate. This is a deliberate *weak
   tiebreaker only*: the label never selects the answer (that was the
   Phase 6 canned behaviour this module replaces), it can only nudge
   otherwise-tied candidates.

Missing-data handling: components that are unavailable for a request
(no graph metadata sent, no incident text) are *excluded* and the
remaining weights renormalised, rather than imputed as zero. A
deployment without a graph adapter is thereby not penalised relative
to one with it — it just ranks on fewer signals.

Confidence
----------

The top candidate's confidence is its score damped by how close the
runner-up is::

    confidence = s1 * (1 - 0.5 * (s2 / s1) ** 2)

With a well-separated runner-up the penalty vanishes (quadratic in the
score ratio); at an exact tie confidence halves. Rationale: a ranking
that cannot discriminate between its top two hypotheses should not
report the same confidence as one that can — the damping approximates
a likelihood-ratio penalty without pretending we have calibrated
likelihoods. Bounds: ``s2 <= s1`` implies ``confidence`` stays within
``[s1/2, s1] ⊆ [0, 1]``.

``matched`` is true only when confidence clears ``THRESHOLD`` (0.5) —
i.e. the top hypothesis is both individually well-supported and
discriminable from its alternatives. An all-zero scoreboard (no
evidence, no graph, no text signal) yields ``method="no_signal"`` and
an ``<unknown>`` answer so callers can distinguish "ranked and won"
from "nothing to rank on".
"""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from typing import Iterable

# ---------------------------------------------------------------------------
# Tunables — every constant is referenced from the module docstring above.
# ---------------------------------------------------------------------------

#: Component weights. Renormalised over the components actually available
#: for a given request (see module docstring, "Missing-data handling").
WEIGHTS: dict[str, float] = {
    "evidence": 0.35,
    "graph": 0.30,
    "text": 0.30,
    "prior": 0.05,
}

#: Confidence a top-ranked hypothesis must clear for ``matched=True``.
THRESHOLD: float = 0.5

#: Evidence-count scale for the saturating volume term 1 - exp(-n/SCALE):
#: three independent evidence items ≈ 0.63 volume, six ≈ 0.86.
_EVIDENCE_SATURATION: float = 3.0

#: Degree-centrality normalisation cap == the Pathfinder traversal edge
#: limit (Provider.Limit in pathfinder/provider.go), so centrality is 1.0
#: exactly when a node touches every edge the traversal could return.
_DEGREE_CAP: int = 20

#: Within the graph component: proximity dominates centrality (see §2).
_GRAPH_PROXIMITY_WEIGHT: float = 0.7
_GRAPH_CENTRALITY_WEIGHT: float = 0.3

#: Specificity anchors — each regex is a class of *verifiable* claim an
#: evidence string can carry. spec(s) = matched classes / len(_SPECIFICITY).
_SPECIFICITY: tuple[re.Pattern[str], ...] = (
    re.compile(r"->"),                     # explicit graph edge a->b
    re.compile(r"(?:\bline\s+\d+)|(?::\d+)"),  # file:line anchor
    re.compile(r"\b[A-Z][a-z]+[A-Z]\w*"),  # CamelCase exception class
    re.compile(r"\b\w+\.\w+"),             # dotted symbol / table.column
)

_TOKEN_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_]{2,}")

#: Tokens too generic to count as corroboration between evidence and
#: incident text (they appear in virtually every trace).
_STOPWORDS: frozenset[str] = frozenset(
    {
        "the", "and", "for", "with", "from", "not", "was", "has", "this",
        "that", "line", "file", "most", "recent", "call", "last", "raise",
        "raised", "error", "errors", "object", "does", "exist", "column",
        "graph", "edge", "src", "demo",
    }
)


# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Candidate:
    """Ranking input — mirrors the wire ``RootCauseCandidate`` model.

    ``in_degree`` / ``out_degree`` / ``distance_from_symptom`` are the
    graph-position metadata the Go Pathfinder computes from its Neo4j
    traversal; all are optional because a graph-less deployment sends
    candidates with evidence only.
    """

    node: str
    evidence: tuple[str, ...] = ()
    in_degree: int | None = None
    out_degree: int | None = None
    distance_from_symptom: int | None = None


@dataclass
class ScoredCandidate:
    """One ranked hypothesis with its per-component score breakdown.

    ``components`` maps component name -> score in [0,1], or ``None``
    when that component was unavailable for the request (and therefore
    excluded from the weighted combination, not counted as zero).
    """

    node: str
    evidence_chain: list[str]
    score: float
    components: dict[str, float | None] = field(default_factory=dict)


@dataclass
class RankingResult:
    """Outcome of ranking one request's candidate set."""

    ranked: list[ScoredCandidate]
    confidence: float
    matched: bool
    method: str  # "graph_evidence_ranking" | "no_signal"

    @property
    def top(self) -> ScoredCandidate | None:
        return self.ranked[0] if self.ranked else None


# ---------------------------------------------------------------------------
# Component scores — each returns a value in [0, 1], or None when the
# signal is unavailable for this candidate/request.
# ---------------------------------------------------------------------------


def _tokens(*texts: str) -> frozenset[str]:
    """Lower-cased identifier tokens (len >= 3) minus stopwords."""

    out: set[str] = set()
    for text in texts:
        for tok in _TOKEN_RE.findall(text):
            low = tok.lower()
            if low not in _STOPWORDS:
                out.add(low)
    return frozenset(out)


def _overlap_coefficient(a: frozenset[str], b: frozenset[str]) -> float:
    """|A ∩ B| / min(|A|, |B|) — asymmetry-tolerant set similarity."""

    if not a or not b:
        return 0.0
    return len(a & b) / min(len(a), len(b))


def evidence_score(cand: Candidate) -> float:
    """Evidence support: saturating volume x anchor specificity.

    A candidate with zero evidence scores 0 (not None): the ranker
    cannot distinguish "upstream attached nothing" from "there was
    nothing to attach", so absent evidence is absent support.
    """

    n = len(cand.evidence)
    if n == 0:
        return 0.0
    volume = 1.0 - math.exp(-n / _EVIDENCE_SATURATION)
    spec_total = 0.0
    for ev in cand.evidence:
        hits = sum(1 for pat in _SPECIFICITY if pat.search(ev))
        spec_total += hits / len(_SPECIFICITY)
    specificity = spec_total / n
    return 0.6 * volume + 0.4 * specificity


def graph_score(cand: Candidate) -> float | None:
    """Graph-structural signal: proximity (0.7) + degree centrality (0.3).

    Returns None when the request carried no graph metadata at all for
    this candidate, so the component is excluded rather than zeroed.
    Distances/degrees refer to the retrieved evidence subgraph (see the
    module docstring) — they come from the Pathfinder's real traversal.
    """

    proximity: float | None = None
    if cand.distance_from_symptom is not None and cand.distance_from_symptom >= 0:
        proximity = 1.0 / (1.0 + cand.distance_from_symptom)

    centrality: float | None = None
    if cand.in_degree is not None or cand.out_degree is not None:
        deg = (cand.in_degree or 0) + (cand.out_degree or 0)
        centrality = min(1.0, math.log1p(deg) / math.log1p(_DEGREE_CAP))

    if proximity is None and centrality is None:
        return None
    if proximity is None:
        return centrality
    if centrality is None:
        return proximity
    return _GRAPH_PROXIMITY_WEIGHT * proximity + _GRAPH_CENTRALITY_WEIGHT * centrality


def text_score(cand: Candidate, incident_tokens: frozenset[str]) -> float | None:
    """Corroboration between candidate tokens and the incident text.

    None when the request carried no incident text (component excluded).
    """

    if not incident_tokens:
        return None
    cand_tokens = _tokens(cand.node, *cand.evidence)
    return _overlap_coefficient(cand_tokens, incident_tokens)


def prior_score(cand: Candidate, scenario_tokens: frozenset[str]) -> float | None:
    """Weak scenario-label prior — a tiebreaker, never the selector.

    Weight 0.05 caps its influence: it can reorder near-ties but cannot
    lift an unsupported candidate over an evidenced one.
    """

    if not scenario_tokens:
        return None
    cand_tokens = _tokens(cand.node, *cand.evidence)
    return _overlap_coefficient(cand_tokens, scenario_tokens)


# ---------------------------------------------------------------------------
# Combination
# ---------------------------------------------------------------------------


def _combine(components: dict[str, float | None]) -> float:
    """Weighted mean over available components, weights renormalised."""

    total_weight = 0.0
    acc = 0.0
    for name, value in components.items():
        if value is None:
            continue
        w = WEIGHTS[name]
        total_weight += w
        acc += w * value
    if total_weight == 0.0:
        return 0.0
    return acc / total_weight


def _contrast_damped_confidence(scores: list[float]) -> float:
    """confidence = s1 * (1 - 0.5 * (s2/s1)^2); see module docstring."""

    if not scores or scores[0] <= 0.0:
        return 0.0
    s1 = scores[0]
    s2 = scores[1] if len(scores) > 1 else 0.0
    ratio = s2 / s1
    conf = s1 * (1.0 - 0.5 * ratio * ratio)
    return max(0.0, min(1.0, conf))


def rank_candidates(
    candidates: Iterable[Candidate],
    incident_text: str = "",
    scenario: str = "",
) -> RankingResult:
    """Ranks candidates and derives a confidence for the winner.

    Deterministic: ties break on (evidence count desc, node name asc)
    so repeated calls with the same payload return the same order.
    An all-zero scoreboard returns method="no_signal" with an empty
    confidence so /infer can answer "<unknown>" honestly.
    """

    incident_tokens = _tokens(incident_text) if incident_text else frozenset()
    # Scenario labels are dash/underscore-separated words, not identifiers.
    scenario_tokens = _tokens(scenario.replace("-", " ").replace("_", " "))

    scored: list[ScoredCandidate] = []
    for cand in candidates:
        components: dict[str, float | None] = {
            "evidence": evidence_score(cand),
            "graph": graph_score(cand),
            "text": text_score(cand, incident_tokens),
            "prior": prior_score(cand, scenario_tokens),
        }
        scored.append(
            ScoredCandidate(
                node=cand.node,
                evidence_chain=list(cand.evidence),
                score=_combine(components),
                components=components,
            )
        )

    scored.sort(key=lambda s: (-s.score, -len(s.evidence_chain), s.node))

    scores = [s.score for s in scored]
    confidence = _contrast_damped_confidence(scores)
    if not scored or scores[0] <= 0.0:
        return RankingResult(ranked=scored, confidence=0.0, matched=False, method="no_signal")
    return RankingResult(
        ranked=scored,
        confidence=confidence,
        matched=confidence >= THRESHOLD,
        method="graph_evidence_ranking",
    )
