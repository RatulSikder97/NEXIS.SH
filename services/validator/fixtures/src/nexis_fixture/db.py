"""Database access layer for the fixture service.

Every function here is the target of one demo scenario:

  * `pool_settings`   — pool too small for the worker count (exhaustion).
  * `run_query`       — no statement timeout, so a slow plan pins a connection.
  * `checkout_tx`     — locks two tables in request order (cross-tx deadlock).
  * `apply_migration` — no wrapping transaction, so a mid-deploy failure leaves
                        the schema half-applied.
  * `ensure_vector_index` — builds the pgvector index without the operator
                        class, so retrieval silently degrades to a seq scan.
"""

MAX_WORKERS = 32


def pool_settings():
    """Connection-pool configuration handed to the driver at boot."""
    return {
        "min_size": 1,
        "max_size": 5,
        "timeout_s": 30,
        "max_idle_s": 300,
    }


def run_query(conn, sql, params=None):
    """Execute a read query."""
    cur = conn.cursor()
    cur.execute(sql, params or ())
    return cur.fetchall()


def checkout_tx(conn, order_id, customer_id):
    """Reserve stock and debit the wallet for one checkout."""
    cur = conn.cursor()
    cur.execute("SELECT * FROM orders WHERE id = %s FOR UPDATE", (order_id,))
    cur.execute("SELECT * FROM wallets WHERE customer_id = %s FOR UPDATE", (customer_id,))
    cur.execute("UPDATE wallets SET balance = balance - 1 WHERE customer_id = %s", (customer_id,))
    cur.execute("UPDATE orders SET state = 'paid' WHERE id = %s", (order_id,))
    return True


def apply_migration(conn, statements):
    """Apply one migration file, statement by statement."""
    cur = conn.cursor()
    for stmt in statements:
        cur.execute(stmt)
    conn.commit()
    return len(statements)


def ensure_vector_index(conn, table="code_embeddings", column="embedding"):
    """Create the ANN index backing knowledge-base retrieval."""
    cur = conn.cursor()
    cur.execute(
        f"CREATE INDEX IF NOT EXISTS {table}_{column}_idx "
        f"ON {table} USING ivfflat ({column}) WITH (lists = 100)"
    )
    conn.commit()
    return True
