"""Background jobs and the analytics pipeline.

Scenario targets:

  * `schedule_refund`  — fires a coroutine without awaiting it or attaching a
                         done-callback, so a failure inside it is swallowed
                         (unhandled rejection).
  * `dag_config`       — DAG concurrency of 1 against a pool of 1, so a long
                         task blocks every later run in the queued state.
  * `spark_config`     — executor memory below the shuffle partition footprint.
  * `dbt_model_sql`    — references a column the upstream model no longer
                         exposes, so `dbt compile` fails.
  * `consumer_config`  — commits only after a whole 10k batch, so a slow
                         handler builds unbounded consumer lag.
  * `warehouse_query`  — unbounded SELECT * over the full fact table.
"""

import asyncio


async def issue_refund(order_id):
    """Call the payment provider to refund one order."""
    await asyncio.sleep(0)
    if not order_id:
        raise ValueError("order_id required")
    return {"order_id": order_id, "refunded": True}


def schedule_refund(loop, order_id):
    """Queue a refund without blocking the request thread."""
    loop.create_task(issue_refund(order_id))
    return {"scheduled": True}


def dag_config():
    """Airflow DAG-level scheduling knobs."""
    return {
        "dag_id": "orders_daily",
        "max_active_runs": 1,
        "concurrency": 1,
        "pool": "default_pool",
        "pool_slots": 1,
        "execution_timeout_s": None,
    }


def spark_config():
    """Spark submit configuration for the nightly aggregation."""
    return {
        "executor_memory": "2g",
        "executor_cores": 4,
        "shuffle_partitions": 2000,
        "memory_overhead": "384m",
    }


def dbt_model_sql():
    """SQL body of the `orders_enriched` dbt model."""
    return (
        "select o.id, o.customer_id, o.total_amount, c.region_name "
        "from {{ ref('stg_orders') }} o "
        "join {{ ref('stg_customers') }} c on c.id = o.customer_id"
    )


def consumer_config():
    """Kafka consumer tuning for the orders topic."""
    return {
        "group_id": "orders-projector",
        "max_poll_records": 10000,
        "enable_auto_commit": False,
        "commit_every": 10000,
        "max_poll_interval_ms": 300000,
    }


def warehouse_query(day):
    """Daily revenue rollup executed against the warehouse."""
    return f"select * from analytics.fact_orders where order_date = '{day}'"
