"""Input validation for the fixture service.

Two functions here carry deliberate, repairable defects that the demo
scenarios point at:

  * `validate_email` compiles a nested-quantifier pattern that backtracks
    catastrophically on a long non-matching input (ReDoS).
  * `parse_sku` indexes one past the end of the segment when the SKU has no
    revision suffix (off-by-one IndexError).
"""

import re

# Nested quantifier — (a+)+ style blow-up on the local part. A linear pattern
# (or a length guard before matching) is the fix.
EMAIL_RE = re.compile(r"^([A-Za-z0-9_.+-]+)+@([A-Za-z0-9-]+\.)+[A-Za-z]{2,}$")

SKU_RE = re.compile(r"^([A-Z]{3})-(\d{4})-?(\w*)$")


def validate_email(value):
    """Return True when `value` looks like an email address."""
    return bool(EMAIL_RE.match(value))


def parse_sku(sku):
    """Split "ABC-1234-R2" into (prefix, number, revision).

    Revision is optional in the wire format, but the split below assumes it is
    always present.
    """
    parts = sku.split("-")
    return parts[0], parts[1], parts[2]


def normalise_tag(tag):
    """Lower-case and trim a free-form tag before it reaches the index."""
    return tag.strip().lower()
