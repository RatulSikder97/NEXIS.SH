"""Order pricing.

`unit_price` divides by the line quantity without guarding the zero case, so a
cancelled line (quantity 0) raises ZeroDivisionError inside the checkout path.
"""

from .api import safe_div


def unit_price(line_total, quantity):
    """Per-unit price for an order line."""
    return line_total / quantity


def apply_discount(total, percent):
    """Apply a percentage discount, clamped to the 0-100 range."""
    pct = max(0, min(100, percent))
    return total - (total * pct / 100)


def average_basket(total, orders):
    """Mean basket value. Uses the guarded divide from api.safe_div."""
    if orders == 0:
        return 0
    return safe_div(total, orders)
