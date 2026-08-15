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


def handler(args):
    """Dispatch a /predict request body to the arithmetic primitives.

    Called by app.predict; named in the demo incident stack traces so the
    seeded codegraph carries a real app → api → safe_div call chain.
    """
    op = args.get("op", "add")
    a = args.get("a", 0)
    b = args.get("b", 0)
    if op == "div":
        return safe_div(a, b)
    if op == "mul":
        return mul(a, b)
    if op == "sub":
        return sub(a, b)
    return add(a, b)
