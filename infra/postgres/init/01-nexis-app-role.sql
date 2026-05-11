-- nexis_app: non-superuser application role used by app services at runtime.
-- The privileged 'nexis' role is reserved for migrations + DDL only.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nexis_app') THEN
    CREATE ROLE nexis_app LOGIN PASSWORD 'nexis_app_dev_password';
  END IF;
END$$;

GRANT CONNECT ON DATABASE nexis TO nexis_app;
GRANT USAGE ON SCHEMA public TO nexis_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO nexis_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO nexis_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexis_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE ON SEQUENCES TO nexis_app;
