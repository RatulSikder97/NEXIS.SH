-- 0007_phase3_5_workspaces.down.sql

DROP TABLE IF EXISTS usage_records;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS payment_methods;
DROP TABLE IF EXISTS workspaces;
ALTER TABLE org_members DROP COLUMN IF EXISTS last_workspace_id;
