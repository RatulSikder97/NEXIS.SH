"""Request entrypoints for the fixture service.

`predict` is the route named by the demo incident stack traces; it walks the
same call chain the fixtures describe (app.predict → api.handler → api.safe_div)
so Pathfinder's graph traversal resolves a real symbol rather than a
placeholder.
"""

from .api import handler


def predict(req):
    """Handle POST /predict. `req` is a mapping with an `args` key."""
    body = handler(req.get("args", {}))
    return {"ok": True, "body": body}


def health(_req=None):
    """Liveness probe used by the preview-deploy engine's health check."""
    return {"status": "ok"}
