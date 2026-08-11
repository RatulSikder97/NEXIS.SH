-- Phase 8 — close the self-healing loop: Synthesiser delegation enforcement.
--
-- The recovery workflow now skips L1 agents the Synthesiser plan left out of
-- selected_agents, and records an explicit 'skipped' activity_events frame so
-- the timeline UI can grey the agent out instead of showing it permanently
-- pending. The original 0009 CHECK predates that state, so widen it here.
-- The constraint name is the Postgres auto-generated one for the inline
-- CHECK in 0009 (activity_events_status_check).

ALTER TABLE activity_events DROP CONSTRAINT IF EXISTS activity_events_status_check;
ALTER TABLE activity_events ADD CONSTRAINT activity_events_status_check
  CHECK (status IN ('started','succeeded','failed','retrying','timed_out','skipped'));
