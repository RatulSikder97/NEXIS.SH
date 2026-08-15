-- Revert to the OpenAI-width embedding column. Vectors from a 768-dim model
-- cannot be widened into 1536-dim OpenAI space, so the table is emptied and
-- must be re-seeded with cmd/seed-pgvector under an OpenAI provider.

DROP INDEX IF EXISTS code_embeddings_embedding_ivfflat;

TRUNCATE TABLE code_embeddings;

ALTER TABLE code_embeddings
  ALTER COLUMN embedding TYPE vector(1536);
