-- Revert OpenLineage persistence. Lineage rows are derived telemetry — the
-- Temporal history + activity_events remain the source of truth for what the
-- Data Engineer proposed — so dropping the table on rollback loses nothing
-- irreplaceable.

DROP TABLE IF EXISTS lineage_events;
