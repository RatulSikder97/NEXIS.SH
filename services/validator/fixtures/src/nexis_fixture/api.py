"""Synthetic arithmetic API used to exercise the validator sandbox.

The functions are deliberately trivial so a patch that breaks one is easy to
construct in Phase 5 demos.
"""

def add(a, b):
    return a + b


def sub(a, b):
    return a - b


def mul(a, b):
    return a * b


def safe_div(a, b):
    if b == 0:
        raise ValueError("division by zero")
    return a / b
