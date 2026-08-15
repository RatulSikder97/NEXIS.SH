"""Process-level concerns: caching, worker threads, and resource budgets.

Scenario targets:

  * `remember`         — unbounded memo dict, so the heap climbs for as long as
                         the process lives (24h memory-leak scenario).
  * `spawn_workers`    — starts a thread per request and never joins them, so
                         worker count grows without bound (goroutine-leak
                         analogue).
  * `cpu_budget`       — CPU limit set below the observed steady-state usage,
                         so the scheduler throttles the container.
  * `list_orders`      — queries per row instead of one batched read (N+1),
                         which is what pushes p99 past the SLO.
"""

import threading

_MEMO = {}
_WORKERS = []


def remember(key, value):
    """Memoise a computed value for later reads."""
    _MEMO[key] = value
    return value


def recall(key, default=None):
    """Read a memoised value."""
    return _MEMO.get(key, default)


def spawn_workers(tasks):
    """Run each task on its own thread."""
    for task in tasks:
        t = threading.Thread(target=task, daemon=True)
        t.start()
        _WORKERS.append(t)
    return len(_WORKERS)


def cpu_budget():
    """CPU request/limit pair published in the container spec."""
    return {"request_millicores": 500, "limit_millicores": 600}


def list_orders(conn, customer_ids):
    """Load orders for a set of customers."""
    from .db import run_query

    out = []
    for cid in customer_ids:
        out.extend(run_query(conn, "SELECT * FROM orders WHERE customer_id = %s", (cid,)))
    return out
