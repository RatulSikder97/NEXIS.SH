-- Revert the 'skipped' widening. Rows already written with status='skipped'
-- would violate the narrowed CHECK, so rewrite them to 'succeeded' first —
-- a skipped agent is a non-event from the DAG's point of view, and losing
-- the distinction is acceptable on a rollback.

UPDATE activity_events SET status = 'succeeded' WHERE status = 'skipped';
ALTER TABLE activity_events DROP CONSTRAINT IF EXISTS activity_events_status_check;
ALTER TABLE activity_events ADD CONSTRAINT activity_events_status_check
  CHECK (status IN ('started','succeeded','failed','retrying','timed_out'));
