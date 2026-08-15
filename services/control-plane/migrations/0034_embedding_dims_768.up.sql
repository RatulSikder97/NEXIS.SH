-- Resize code_embeddings.embedding from vector(1536) to vector(768).
--
-- 0012 hardcoded 1536, the width of OpenAI text-embedding-3-small. The
-- default local provider is Ollama + nomic-embed-text, which emits 768, so
-- every insert failed with "expected 1536 dimensions, not 768" and the
-- retrieval store stayed empty — which in turn left the code agents with no
-- source context and made them hallucinate the file they were patching.
--
-- The column width has to match the configured embedding provider; pgvector
-- needs a fixed dimension for the ivfflat index, so it cannot be provider
-- agnostic. This pins the store to 768-dim embeddings. Switching back to
-- OpenAI embeddings requires the mirror migration (see .down.sql) plus a
-- full re-seed, because vectors from different models are not comparable.
--
-- Existing rows cannot be converted between models, so the table is emptied
-- rather than cast; re-run cmd/seed-pgvector afterwards.

DROP INDEX IF EXISTS code_embeddings_embedding_ivfflat;

TRUNCATE TABLE code_embeddings;

ALTER TABLE code_embeddings
  ALTER COLUMN embedding TYPE vector(768);
