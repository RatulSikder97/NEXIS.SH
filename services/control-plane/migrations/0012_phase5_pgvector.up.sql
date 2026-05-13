-- Phase 5 — pgvector extension + code_embeddings table.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE code_embeddings (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id),
  repo_sha      text NOT NULL,
  file_path     text NOT NULL,
  chunk_start   int  NOT NULL,
  chunk_end     int  NOT NULL,
  content       text NOT NULL,
  embedding     vector(1536) NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX code_embeddings_org_repo_idx ON code_embeddings (org_id, repo_sha);

-- ivfflat index is created post-seed (centroid initialization requires data).
-- See cmd/seed-pgvector/main.go.
