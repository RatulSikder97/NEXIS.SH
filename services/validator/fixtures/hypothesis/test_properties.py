"""Phase 6 Hypothesis property tests for the nexis_fixture API.

The suite runs inside the Hypothesis sidecar against the patched
fixture tree on PYTHONPATH (default ``/srv/fixture``). It is
deliberately tiny — Phase 7 swaps to an auto-generated suite once the
typed-Python static-analysis generator lands.

Per Phase 6 constraints:
  - Test budget capped at 30 seconds by the sidecar (deadline_ms env).
  - Each property assertion mirrors the behaviour of the un-patched
    fixture so a *broken* patch falsifies the property.
"""

from __future__ import annotations

import os

import pytest
from hypothesis import given, settings, strategies as st

# Import the patched fixture. The sidecar prepends ``repo_path`` to
# PYTHONPATH so this resolves to the in-container copy of ``src/``.
nexis_fixture = pytest.importorskip(
    "nexis_fixture",
    reason="nexis_fixture not on PYTHONPATH — check sidecar repo_path wiring",
)


_DEADLINE_MS = int(os.environ.get("HYPOTHESIS_DEADLINE_MS", "30000"))
_MAX_EXAMPLES = int(os.environ.get("HYPOTHESIS_MAX_EXAMPLES", "20"))

# Bounded integers — keeps each Hypothesis run inside the 30 s budget by
# capping the search space, while still covering the negative / zero /
# positive partitions the L1 patches typically touch.
_ints = st.integers(min_value=-10_000, max_value=10_000)


@settings(max_examples=_MAX_EXAMPLES, deadline=_DEADLINE_MS)
@given(_ints, _ints)
def test_add_is_commutative(a: int, b: int) -> None:
    assert nexis_fixture.add(a, b) == nexis_fixture.add(b, a)


@settings(max_examples=_MAX_EXAMPLES, deadline=_DEADLINE_MS)
@given(_ints, _ints)
def test_sub_is_inverse_of_add(a: int, b: int) -> None:
    assert nexis_fixture.sub(nexis_fixture.add(a, b), b) == a


@settings(max_examples=_MAX_EXAMPLES, deadline=_DEADLINE_MS)
@given(_ints, _ints)
def test_mul_is_commutative(a: int, b: int) -> None:
    assert nexis_fixture.mul(a, b) == nexis_fixture.mul(b, a)


@settings(max_examples=_MAX_EXAMPLES, deadline=_DEADLINE_MS)
@given(_ints, _ints.filter(lambda b: b != 0))
def test_safe_div_round_trip(a: int, b: int) -> None:
    # safe_div(a, b) * b should recover ``a`` for integer inputs that
    # divide cleanly. We only assert for the divisible case so the
    # property isn't sensitive to float precision.
    if a % b != 0:
        return
    assert nexis_fixture.safe_div(a, b) * b == a


@settings(max_examples=_MAX_EXAMPLES, deadline=_DEADLINE_MS)
@given(_ints)
def test_safe_div_zero_always_raises(a: int) -> None:
    # safe_div must raise on b==0 for every numerator. A patch that
    # short-circuits to 0 / None must falsify this property.
    with pytest.raises((ValueError, ZeroDivisionError)):
        nexis_fixture.safe_div(a, 0)
