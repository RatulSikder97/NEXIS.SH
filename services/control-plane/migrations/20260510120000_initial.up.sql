-- NEXIS initial schema (full §3 of the design spec)
-- Multi-tenant, RLS-enforced, audit-everything.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "citext";
CREATE EXTENSION IF NOT EXISTS "vector";

CREATE SCHEMA IF NOT EXISTS audit;

-- ============================================================================
-- TENANCY
-- ============================================================================

CREATE TABLE orgs (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  clerk_org_id TEXT UNIQUE,
  name TEXT NOT NULL,
  slug TEXT UNIQUE NOT NULL,
  plan TEXT NOT NULL DEFAULT 'design_partner',
  encrypted_dek BYTEA,
  encryption_key_id TEXT,
  settings JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);

CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  clerk_user_id TEXT UNIQUE NOT NULL,
  email CITEXT UNIQUE NOT NULL,
  name TEXT,
  avatar_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);

CREATE TABLE org_members (
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('owner','admin','sre','engineer','reviewer','viewer','bot')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX idx_org_members_user ON org_members(user_id);

CREATE TABLE teams (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)
);

CREATE TABLE team_members (
  team_id UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (team_id, user_id)
);

-- ============================================================================
-- AUTH (Clerk-synced sessions + NEXIS-owned API keys)
-- ============================================================================

CREATE TABLE auth_sessions (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id UUID REFERENCES orgs(id) ON DELETE SET NULL,
  clerk_session_id TEXT UNIQUE NOT NULL,
  ip INET,
  user_agent TEXT,
  device_label TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);
CREATE INDEX idx_auth_sessions_user ON auth_sessions(user_id, created_at DESC);

CREATE TABLE api_keys (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  created_by_user_id UUID NOT NULL REFERENCES users(id),
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  scopes JSONB NOT NULL DEFAULT '[]',
  service_scope JSONB,
  ip_allowlist INET[],
  expires_at TIMESTAMPTZ NOT NULL,
  last_used_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_api_keys_org ON api_keys(org_id, revoked_at);
CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix) WHERE revoked_at IS NULL;

-- Idempotency table for Clerk webhook delivery
CREATE TABLE clerk_webhook_events (
  id BIGSERIAL PRIMARY KEY,
  clerk_event_id TEXT UNIQUE NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- DOMAIN: services, connectors
-- ============================================================================

CREATE TABLE services (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  repo_url TEXT,
  owner_team_id UUID REFERENCES teams(id),
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)
);

CREATE TABLE connectors (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  config JSONB NOT NULL DEFAULT '{}',
  encrypted_secret BYTEA,
  encryption_key_id TEXT,
  is_demo BOOLEAN NOT NULL DEFAULT false,
  health TEXT NOT NULL DEFAULT 'unknown',
  last_checked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, kind, name)
);
CREATE INDEX idx_connectors_org ON connectors(org_id);

-- ============================================================================
-- INCIDENTS + RECOVERY-LOOP ARTIFACTS
-- ============================================================================

CREATE TABLE incidents (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  service_id UUID REFERENCES services(id) ON DELETE SET NULL,
  workflow_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  status TEXT NOT NULL,
  severity TEXT NOT NULL CHECK (severity IN ('low','medium','high','critical')),
  source TEXT NOT NULL,
  source_event_id TEXT,
  title TEXT NOT NULL,
  summary TEXT,
  is_demo BOOLEAN NOT NULL DEFAULT false,
  scenario_id TEXT,
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);
CREATE INDEX idx_incidents_org_status ON incidents(org_id, status, detected_at DESC);
CREATE INDEX idx_incidents_org_demo ON incidents(org_id, is_demo, detected_at DESC);
CREATE INDEX idx_incidents_workflow ON incidents(workflow_id);

CREATE TABLE root_cause_reports (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  hypotheses JSONB NOT NULL,
  blast_radius JSONB,
  counterfactual_evidence JSONB,
  confidence NUMERIC(3,2),
  produced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_rca_incident ON root_cause_reports(incident_id);

CREATE TABLE patch_candidates (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  ordinal INT NOT NULL,
  diff TEXT NOT NULL,
  explanation TEXT NOT NULL,
  contracts_touched JSONB,
  llm_model TEXT NOT NULL,
  llm_tokens INT,
  llm_cache_hit BOOLEAN,
  produced_by_agent TEXT,
  produced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (incident_id, ordinal)
);
CREATE INDEX idx_patch_incident ON patch_candidates(incident_id);

CREATE TABLE validation_runs (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  patch_candidate_id UUID NOT NULL REFERENCES patch_candidates(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  test_summary JSONB,
  shadow_confidence NUMERIC(3,2),
  evidence_s3_key TEXT,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);
CREATE INDEX idx_validation_patch ON validation_runs(patch_candidate_id);

CREATE TABLE approval_decisions (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  patch_candidate_id UUID REFERENCES patch_candidates(id) ON DELETE SET NULL,
  decided_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  decision TEXT NOT NULL CHECK (decision IN ('approve','reject','auto_approve','escalate')),
  policy_id UUID,
  rationale TEXT,
  decided_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_approval_incident ON approval_decisions(incident_id);

CREATE TABLE deployments (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  patch_candidate_id UUID NOT NULL REFERENCES patch_candidates(id) ON DELETE CASCADE,
  pr_url TEXT NOT NULL,
  pr_number INT NOT NULL,
  commit_sha TEXT,
  argocd_app TEXT,
  health TEXT NOT NULL DEFAULT 'pending',
  rollback_reason TEXT,
  opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  merged_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ
);
CREATE INDEX idx_deployments_incident ON deployments(incident_id);

CREATE TABLE approval_policies (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  matcher JSONB NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('auto_approve','auto_merge','require_human','escalate')),
  required_role TEXT,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_policies_org ON approval_policies(org_id, enabled);

-- ============================================================================
-- AGENTS — fleet panel, RLHF
-- ============================================================================

CREATE TABLE agent_runs (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  agent TEXT NOT NULL,
  layer TEXT NOT NULL CHECK (layer IN ('L1','L2','platform')),
  workflow_id TEXT,
  incident_id UUID REFERENCES incidents(id) ON DELETE SET NULL,
  status TEXT NOT NULL,
  input_summary JSONB,
  output_summary JSONB,
  delegated_to TEXT,
  llm_tokens_in INT,
  llm_tokens_out INT,
  llm_cache_hit BOOLEAN,
  duration_ms INT,
  started_at TIMESTAMPTZ NOT NULL,
  finished_at TIMESTAMPTZ
);
CREATE INDEX idx_agent_runs_org_agent ON agent_runs(org_id, agent, started_at DESC);
CREATE INDEX idx_agent_runs_incident ON agent_runs(incident_id);

CREATE TABLE feedback_examples (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  agent TEXT NOT NULL,
  input_artifact JSONB NOT NULL,
  output_artifact JSONB NOT NULL,
  reward NUMERIC(3,2) NOT NULL,
  reward_source TEXT NOT NULL,
  decided_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  incident_id UUID REFERENCES incidents(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_feedback_org_agent ON feedback_examples(org_id, agent, reward DESC, created_at DESC);

CREATE TABLE prompt_versions (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  agent TEXT NOT NULL,
  version INT NOT NULL,
  system_prompt TEXT NOT NULL,
  example_ids UUID[],
  dataset_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('shadow','active','retired')),
  promoted_at TIMESTAMPTZ,
  promoted_by_user_id UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (agent, version)
);

-- ============================================================================
-- VECTOR MEMORY (Synthesiser fallback when Pinecone unavailable)
-- ============================================================================

CREATE TABLE memory_documents (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  ref_id TEXT,
  content TEXT NOT NULL,
  embedding vector(1536),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_memory_embedding ON memory_documents USING ivfflat (embedding vector_cosine_ops);
CREATE INDEX idx_memory_org_kind ON memory_documents(org_id, kind);

-- ============================================================================
-- AUDIT — append-only
-- ============================================================================

CREATE TABLE audit.audit_events (
  id BIGSERIAL PRIMARY KEY,
  org_id UUID,
  actor_user_id UUID,
  actor_kind TEXT NOT NULL,
  action TEXT NOT NULL,
  resource_kind TEXT NOT NULL,
  resource_id TEXT,
  payload JSONB,
  ip INET,
  user_agent TEXT,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_org_time ON audit.audit_events(org_id, occurred_at DESC);
CREATE INDEX idx_audit_resource ON audit.audit_events(resource_kind, resource_id);

-- ============================================================================
-- ROW-LEVEL SECURITY (defense-in-depth)
-- ============================================================================

ALTER TABLE incidents             ENABLE ROW LEVEL SECURITY;
ALTER TABLE root_cause_reports    ENABLE ROW LEVEL SECURITY;
ALTER TABLE patch_candidates      ENABLE ROW LEVEL SECURITY;
ALTER TABLE validation_runs       ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_decisions    ENABLE ROW LEVEL SECURITY;
ALTER TABLE deployments           ENABLE ROW LEVEL SECURITY;
ALTER TABLE approval_policies     ENABLE ROW LEVEL SECURITY;
ALTER TABLE services              ENABLE ROW LEVEL SECURITY;
ALTER TABLE connectors            ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_runs            ENABLE ROW LEVEL SECURITY;
ALTER TABLE feedback_examples     ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_documents      ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys              ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_members           ENABLE ROW LEVEL SECURITY;
ALTER TABLE teams                 ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth_sessions         ENABLE ROW LEVEL SECURITY;

-- Reusable policy template — every tenant table reads/writes scoped to current org.
DO $$
DECLARE t TEXT;
DECLARE org_tables TEXT[] := ARRAY[
  'incidents','root_cause_reports','patch_candidates','validation_runs',
  'approval_decisions','deployments','approval_policies','services','connectors',
  'agent_runs','feedback_examples','memory_documents','api_keys','org_members','teams'
];
BEGIN
  FOREACH t IN ARRAY org_tables LOOP
    EXECUTE format(
      'CREATE POLICY %I_tenant_isolation ON %I USING (org_id = current_setting(''app.current_org_id'', true)::uuid);',
      t, t
    );
  END LOOP;
END $$;

-- Auth sessions are scoped to the user, not org
CREATE POLICY auth_sessions_user_isolation ON auth_sessions
  USING (user_id = current_setting('app.current_user_id', true)::uuid);

-- Allow nexis-internal admin role to bypass RLS via SECURITY DEFINER functions
-- (defined in a later migration alongside the admin service).
