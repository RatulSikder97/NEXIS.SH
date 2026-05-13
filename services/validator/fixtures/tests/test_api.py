import pytest

from nexis_fixture import add, sub, mul, safe_div


# 12 trivial cases exercising the four primitives plus the ValueError path.
def test_add_positive():
    assert add(2, 3) == 5


def test_add_negative():
    assert add(-1, -1) == -2


def test_add_zero():
    assert add(0, 0) == 0


def test_sub_positive():
    assert sub(5, 3) == 2


def test_sub_negative():
    assert sub(-1, -1) == 0


def test_sub_zero():
    assert sub(0, 0) == 0


def test_mul_positive():
    assert mul(2, 3) == 6


def test_mul_zero():
    assert mul(2, 0) == 0


def test_mul_negative():
    assert mul(-2, 3) == -6


def test_safe_div_positive():
    assert safe_div(6, 3) == 2


def test_safe_div_float():
    assert abs(safe_div(1, 4) - 0.25) < 1e-9


def test_safe_div_zero_raises():
    with pytest.raises(ValueError):
        safe_div(1, 0)
