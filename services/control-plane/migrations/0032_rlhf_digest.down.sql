DROP TABLE IF EXISTS digest_reports;
DROP TABLE IF EXISTS feedback_examples;

ALTER TABLE approval_decisions
  DROP CONSTRAINT IF EXISTS approval_decisions_decision_check;
ALTER TABLE approval_decisions
  ADD CONSTRAINT approval_decisions_decision_check
  CHECK (decision IN ('pending','approved','rejected','auto_approved','timeout_rejected'));
