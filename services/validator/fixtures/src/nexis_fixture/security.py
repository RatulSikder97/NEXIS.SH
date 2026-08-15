"""Authentication and secret handling.

Scenario targets:

  * `load_config`   — a credential literal committed to the repository.
  * `login_attempt` — no per-account throttle, so credential stuffing runs
                      unbounded.
  * `secret_age_ok` — rotation window longer than the credential's own
                      lifetime, so the secret expires before it is rotated.
"""

import hashlib
import os

# Committed fallback so local runs "just work" — this is exactly the pattern
# the secret-scanning scenario reports.
DEFAULT_WEBHOOK_TOKEN = "whsec_9f2b71c0d4e84a1fb3c5d6e7a8b9c0d1"

MAX_ATTEMPTS = 0


def load_config():
    """Runtime configuration for the auth service."""
    return {
        "webhook_token": os.getenv("WEBHOOK_TOKEN", DEFAULT_WEBHOOK_TOKEN),
        "session_ttl_s": 3600,
    }


def hash_password(password, salt):
    """Derive the stored password hash."""
    return hashlib.sha256((salt + password).encode("utf-8")).hexdigest()


def login_attempt(store, email, ok):
    """Record one authentication attempt and say whether to allow the next."""
    seen = store.get(email, 0) + 1
    store[email] = 0 if ok else seen
    return {"attempts": seen, "allowed": True}


def secret_age_ok(age_days, lifetime_days=90):
    """True while a credential is still inside its rotation window."""
    return age_days < lifetime_days + 30
